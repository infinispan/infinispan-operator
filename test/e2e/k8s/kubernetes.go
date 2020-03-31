package k8s

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/k8s"
	tconstants "github.com/infinispan/infinispan-operator/test/e2e/constants"
	"github.com/infinispan/infinispan-operator/test/e2e/utils"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

// Runtime scheme
var scheme = runtime.NewScheme()

var log = logf.Log.WithName("kubernetes_test")

// TestKubernetes abstracts testing related interaction with a Kubernetes cluster
type TestKubernetes struct {
	Kubernetes *k8s.Kubernetes
}

func init() {
	addToScheme(&v1.SchemeBuilder, scheme)
	addToScheme(&rbacv1.SchemeBuilder, scheme)
	addToScheme(&apiextv1beta1.SchemeBuilder, scheme)
	addToScheme(&ispnv1.SchemeBuilder.SchemeBuilder, scheme)
	addToScheme(&appsv1.SchemeBuilder, scheme)
}

func addToScheme(schemeBuilder *runtime.SchemeBuilder, scheme *runtime.Scheme) {
	err := schemeBuilder.AddToScheme(scheme)
	utils.ExpectNoError(err)
}

// NewTestKubernetes creates a new instance of TestKubernetes
func NewTestKubernetes() *TestKubernetes {
	mapperProvider := apiutil.NewDynamicRESTMapper
	kubernetes, err := NewKubernetesFromLocalConfig(scheme, mapperProvider)
	utils.ExpectNoError(err)
	return &TestKubernetes{Kubernetes: kubernetes}
}

// NewNamespace creates a new namespace
func (k TestKubernetes) NewNamespace(namespace string) {
	fmt.Printf("Create namespace %s\n", namespace)
	obj := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	ns := types.NamespacedName{Name: namespace, Namespace: namespace}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, obj)
	if err != nil && errors.IsNotFound(err) {
		err = k.Kubernetes.Client.Create(context.TODO(), obj)
		utils.ExpectNoError(err)
		return
	}

	utils.ExpectNoError(err)
}

// DeleteNamespace deletes a namespace
func (k TestKubernetes) DeleteNamespace(namespace string) {
	fmt.Printf("Delete namespace %s\n", namespace)
	obj := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespace,
			Namespace: namespace,
		},
	}
	err := k.Kubernetes.Client.Delete(context.TODO(), obj, tconstants.DeleteOpts...)
	utils.ExpectMaybeNotFound(err)

	fmt.Println("Waiting for the namespace to be removed")
	ns := types.NamespacedName{Name: namespace, Namespace: namespace}
	err = wait.Poll(tconstants.DefaultPollPeriod, tconstants.MaxWaitTimeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), ns, obj)
		if err != nil && errors.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	})
	utils.ExpectNoError(err)
}

// CreateInfinispan creates an Infinispan resource in the given namespace
func (k TestKubernetes) CreateInfinispan(infinispan *ispnv1.Infinispan, namespace string) {
	infinispan.Namespace = namespace
	err := k.Kubernetes.Client.Create(context.TODO(), infinispan)
	utils.ExpectNoError(err)
}

// DeleteInfinispan deletes the infinispan resource
// and waits that all the pods are gone
func (k TestKubernetes) DeleteInfinispan(infinispan *ispnv1.Infinispan, timeout time.Duration) {
	err := k.Kubernetes.Client.Delete(context.TODO(), infinispan, tconstants.DeleteOpts...)
	utils.ExpectNoError(err)

	// TODO getting list of infinispan pods is also done in controller (refactor)
	podList := &v1.PodList{}
	infinispanLabels := map[string]string{"app": "infinispan-pod"}
	labelSelector := labels.SelectorFromSet(infinispanLabels)
	listOps := &client.ListOptions{Namespace: infinispan.Namespace, LabelSelector: labelSelector}
	err = wait.Poll(tconstants.DefaultPollPeriod, timeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.List(context.TODO(), podList, listOps)
		pods := podList.Items
		if err != nil || len(pods) != 0 {
			return false, nil
		}
		return true, nil
	})
	utils.ExpectNoError(err)
	// Check that PersistentVolumeClaims have been cleanup
	err = wait.Poll(tconstants.DefaultPollPeriod, timeout, func() (done bool, err error) {
		pvc := &v1.PersistentVolumeClaimList{}
		err = k.Kubernetes.Client.List(context.TODO(), pvc, listOps)
		pvcs := pvc.Items
		if err != nil || len(pvcs) != 0 {
			return false, nil
		}
		return true, nil
	})
	utils.ExpectNoError(err)
}

// GracefulShutdownInfinispan deletes the infinispan resource
// and waits that all the pods are gone
func (k TestKubernetes) GracefulShutdownInfinispan(infinispan *ispnv1.Infinispan, timeout time.Duration) {
	ns := types.NamespacedName{Name: infinispan.Name, Namespace: infinispan.Namespace}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
	utils.ExpectNoError(err)
	infinispan.Spec.Replicas = 0

	// Workaround for OpenShift local test (clear GVK on decode in the client)
	infinispan.TypeMeta = tconstants.InfinispanTypeMeta
	err = k.Kubernetes.Client.Update(context.TODO(), infinispan)
	utils.ExpectNoError(err)

	err = wait.Poll(tconstants.DefaultPollPeriod, timeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
		c := infinispan.GetCondition("gracefulShutdown")
		if err != nil || c == nil || *c != "True" {
			return false, nil
		}
		return true, nil
	})

	utils.ExpectNoError(err)
}

// GracefulRestartInfinispan restarts the infinispan resource
// and waits that cluster is wellformed
func (k TestKubernetes) GracefulRestartInfinispan(infinispan *ispnv1.Infinispan, replicas int32, timeout time.Duration) {
	ns := types.NamespacedName{Name: infinispan.Name, Namespace: infinispan.Namespace}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
	utils.ExpectNoError(err)
	infinispan.Spec.Replicas = replicas
	err = wait.Poll(tconstants.DefaultPollPeriod, timeout, func() (done bool, err error) {
		// Workaround for OpenShift local test (clear GVK on decode in the client)
		infinispan.TypeMeta = tconstants.InfinispanTypeMeta
		err1 := k.Kubernetes.Client.Update(context.TODO(), infinispan)
		if err1 != nil {
			fmt.Printf("Error updating infinispan %s\n", err1)
			err1 = k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
			infinispan.Spec.Replicas = replicas
			return false, nil
		}
		return true, nil
	})

	utils.ExpectNoError(err)

	err = wait.Poll(tconstants.DefaultPollPeriod, timeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
		c := infinispan.GetCondition("wellFormed")
		if err != nil || c == nil || *c != "True" {
			return false, nil
		}
		return true, nil
	})

	utils.ExpectNoError(err)
}

// CreateOrUpdateRole creates or updates a Role in the given namespace
func (k TestKubernetes) CreateOrUpdateRole(role *rbacv1.Role, namespace string) {
	ns := types.NamespacedName{Name: role.ObjectMeta.Name, Namespace: namespace}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, role)
	if err != nil && errors.IsNotFound(err) {
		role.Namespace = namespace
		err = k.Kubernetes.Client.Create(context.TODO(), role)
		utils.ExpectNoError(err)
		return
	} else if err == nil {
		err = k.Kubernetes.Client.Update(context.TODO(), role)
		utils.ExpectNoError(err)
		return
	}

	utils.ExpectNoError(err)
}

// CreateOrUpdateSa creates or updates a ServiceAccount in the given namespace
func (k TestKubernetes) CreateOrUpdateSa(sa *v1.ServiceAccount, namespace string) {
	ns := types.NamespacedName{Name: sa.ObjectMeta.Name, Namespace: namespace}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, sa)
	if err != nil && errors.IsNotFound(err) {
		sa.Namespace = namespace
		err = k.Kubernetes.Client.Create(context.TODO(), sa)
		utils.ExpectNoError(err)
		return
	} else if err == nil {
		err = k.Kubernetes.Client.Update(context.TODO(), sa)
		utils.ExpectNoError(err)
		return
	}

	utils.ExpectNoError(err)
}

// CreateOrUpdateRoleBinding creates or updates a RoleBinding in the given namespace
func (k TestKubernetes) CreateOrUpdateRoleBinding(binding *rbacv1.RoleBinding, namespace string) {
	ns := types.NamespacedName{Name: binding.ObjectMeta.Name, Namespace: namespace}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, binding)
	if err != nil && errors.IsNotFound(err) {
		binding.Namespace = namespace
		err = k.Kubernetes.Client.Create(context.TODO(), binding)
		utils.ExpectNoError(err)
		return
	} else if err == nil {
		err = k.Kubernetes.Client.Update(context.TODO(), binding)
		utils.ExpectNoError(err)
		return
	}

	utils.ExpectNoError(err)
}

// CreateAndWaitForCRD creates a Custom Resource Definition, waiting it to become ready.
func (k TestKubernetes) CreateAndWaitForCRD(crd *apiextv1beta1.CustomResourceDefinition, namespace string) {
	fmt.Printf("Create CRD %s\n", crd.ObjectMeta.Name)

	ns := types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: namespace}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, crd)
	if err != nil && errors.IsNotFound(err) {
		err = k.Kubernetes.Client.Create(context.TODO(), crd)
		utils.ExpectNoError(err)
	}

	fmt.Println("Wait for CRD to created")
	err = wait.Poll(tconstants.DefaultPollPeriod, tconstants.MaxWaitTimeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), ns, crd)
		if err != nil {
			return false, fmt.Errorf("unable to get CRD: %v", err)
		}

		for _, cond := range crd.Status.Conditions {
			switch cond.Type {
			case apiextv1beta1.Established:
				if cond.Status == apiextv1beta1.ConditionTrue {
					fmt.Println("CRD established")
					return true, nil
				}
			case apiextv1beta1.NamesAccepted:
				if cond.Status == apiextv1beta1.ConditionFalse {
					return false, fmt.Errorf("naming conflict detected for CRD %s", crd.GetName())
				}
			}
		}

		return false, nil
	})

	utils.ExpectNoError(err)
}

// WaitForExternalService checks if an http server is listening at the endpoint exposed by the service (ns, name)
func (k TestKubernetes) WaitForExternalService(routeName string, timeout time.Duration, client *http.Client, user, password, namespace string) string {
	var hostAndPort string
	err := wait.Poll(tconstants.DefaultPollPeriod, timeout, func() (done bool, err error) {
		route := &v1.Service{}
		ns := types.NamespacedName{Name: routeName, Namespace: namespace}
		err = k.Kubernetes.Client.Get(context.TODO(), ns, route)
		utils.ExpectNoError(err)

		// depending on the k8s cluster setting, service is sometime available
		// via Ingress address sometime via node port. So trying both the methods
		// Try node port first
		hostAndPort = fmt.Sprintf("%s:%d", k.Kubernetes.PublicIP(), k.Kubernetes.GetNodePort(route))
		result := checkExternalAddress(client, user, password, hostAndPort, log)
		if result {
			return result, nil
		}

		// if node port fails and it points to 127.0.0.1,
		// it could be running kubernetes-inside-docker,
		// so try using cluster ip instead
		if strings.Contains(hostAndPort, "127.0.0.1") {
			log.Info("Running on kind (kubernetes-inside-docker), try accessing via node port forward")
			hostAndPort =  fmt.Sprintf("127.0.0.1:%s", tconstants.DefaultClusterPort)
			result := checkExternalAddress(client, user, password, hostAndPort, log)
			if result {
				return result, nil
			}
		}

		// then try to get ingress information
		hostAndPort, err = k.getExternalAddress(route, namespace)
		if err != nil {
			return false, nil
		}
		return checkExternalAddress(client, user, password, hostAndPort, log), nil
	})
	utils.ExpectNoError(err)
	return hostAndPort
}

func (k TestKubernetes) getExternalAddress(route *v1.Service, namespace string) (string, error) {
	// If the cluster exposes external IP then return it
	if len(route.Status.LoadBalancer.Ingress) == 1 {
		if route.Status.LoadBalancer.Ingress[0].IP != "" {
			return fmt.Sprintf("%s:%s", route.Status.LoadBalancer.Ingress[0].IP, tconstants.DefaultClusterPort), nil
		}
		if route.Status.LoadBalancer.Ingress[0].Hostname != "" {
			return fmt.Sprintf("%s:%s", route.Status.LoadBalancer.Ingress[0].Hostname,  tconstants.DefaultClusterPort), nil
		}
	}

	// Return empty address if nothing available
	return "", fmt.Errorf("external address not found")
}

// WaitForPods waits for pods in the given namespace, having a certain label to reach the desired count in ContainersReady state
func (k TestKubernetes) WaitForPods(label string, required int, timeout time.Duration, namespace string) {
	labelSelector, err := labels.Parse(label)
	utils.ExpectNoError(err)

	listOps := &client.ListOptions{Namespace: namespace, LabelSelector: labelSelector}
	podList := &v1.PodList{}
	err = wait.Poll(tconstants.DefaultPollPeriod, timeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.List(context.TODO(), podList, listOps)
		if err != nil {
			return false, nil
		}
		pods := podList.Items

		if len(pods) != required {
			debugPods(required, pods)
			return false, nil
		}
		allReady := true
		for _, pod := range pods {
			podReady := false
			conditions := pod.Status.Conditions
			for _, cond := range conditions {
				if (cond.Type) == v1.ContainersReady && cond.Status == "True" {
					podReady = true
				}
			}
			allReady = allReady && podReady
		}
		return allReady, nil
	})

	utils.ExpectNoError(err)
}

// DeleteCRD deletes a CustomResourceDefinition
func (k TestKubernetes) DeleteCRD(name string) {
	crd := &apiextv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	err := k.Kubernetes.Client.Delete(context.TODO(), crd, tconstants.DeleteOpts...)
	utils.ExpectMaybeNotFound(err)
}

// Nodes returns a list of running nodes in the cluster
func (k TestKubernetes) Nodes() []string {
	nodes := &v1.NodeList{}
	listOps := &client.ListOptions{}
	err := k.Kubernetes.Client.List(context.TODO(), nodes, listOps)
	utils.ExpectNoError(err)

	var s []string
	if nodes.Size() == 0 {
		return s
	}
	for _, element := range nodes.Items {
		s = append(s, element.Name)
	}
	return s
}

// GetPods return an array of pods matching the selector label in the given namespace
func (k TestKubernetes) GetPods(label, namespace string) []v1.Pod {
	labelSelector, err := labels.Parse(label)
	utils.ExpectNoError(err)

	listOps := &client.ListOptions{Namespace: namespace, LabelSelector: labelSelector}
	pods := &v1.PodList{}
	err = k.Kubernetes.Client.List(context.TODO(), pods, listOps)
	utils.ExpectNoError(err)

	return pods.Items
}

// CreateSecret creates a secret
func (k TestKubernetes) CreateSecret(secret *v1.Secret, namespace string) {
	secret.Namespace = namespace
	err := k.Kubernetes.Client.Create(context.TODO(), secret)
	utils.ExpectNoError(err)
}

// DeleteSecret deletes a secret
func (k TestKubernetes) DeleteSecret(secret *v1.Secret) {
	err := k.Kubernetes.Client.Delete(context.TODO(), secret, tconstants.DeleteOpts...)
	utils.ExpectNoError(err)
}

// RunOperator runs an operator on a Kubernetes cluster
func (k TestKubernetes) RunOperator(namespace string) chan struct{} {
	k.installRBAC(namespace)
	k.installCRD(namespace)
	stopCh := make(chan struct{})
	go runOperatorLocally(stopCh, namespace)
	return stopCh
}

// Install resources from rbac.yaml required by the Infinispan operator
func (k TestKubernetes) installRBAC(namespace string) {
	yamlReader, err := utils.GetYamlReaderFromFile("../../deploy/role.yaml")
	utils.ExpectNoError(err)
	read, _ := yamlReader.Read()
	role := rbacv1.Role{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read))).Decode(&role)
	utils.ExpectNoError(err)
	k.CreateOrUpdateRole(&role, namespace)

	yamlReader, err = utils.GetYamlReaderFromFile("../../deploy/service_account.yaml")
	utils.ExpectNoError(err)
	read2, _ := yamlReader.Read()
	sa := v1.ServiceAccount{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read2))).Decode(&sa)
	utils.ExpectNoError(err)
	k.CreateOrUpdateSa(&sa, namespace)

	yamlReader, err = utils.GetYamlReaderFromFile("../../deploy/role_binding.yaml")
	utils.ExpectNoError(err)
	read3, _ := yamlReader.Read()
	binding := rbacv1.RoleBinding{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read3))).Decode(&binding)
	utils.ExpectNoError(err)
	k.CreateOrUpdateRoleBinding(&binding, namespace)
}

func (k TestKubernetes) installCRD(namespace string) {
	yamlReader, err := utils.GetYamlReaderFromFile("../../deploy/crds/infinispan.org_infinispans_crd.yaml")
	read, _ := yamlReader.Read()
	crd := apiextv1beta1.CustomResourceDefinition{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read))).Decode(&crd)
	utils.ExpectNoError(err)
	k.CreateAndWaitForCRD(&crd, namespace)
	crdInfinispan := apiextv1beta1.CustomResourceDefinition{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read))).Decode(&crdInfinispan)
	utils.ExpectNoError(err)
	k.CreateAndWaitForCRD(&crdInfinispan, namespace)

	yamlReader, err = utils.GetYamlReaderFromFile("../../deploy/crds/infinispan.org_caches_crd.yaml")
	read, _ = yamlReader.Read()
	crdCache := apiextv1beta1.CustomResourceDefinition{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read))).Decode(&crdCache)
	utils.ExpectNoError(err)
	k.CreateAndWaitForCRD(&crdCache, namespace)
}
