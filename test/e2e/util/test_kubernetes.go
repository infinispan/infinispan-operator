package util

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	"github.com/infinispan/infinispan-operator/pkg/launcher"
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
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

// Default retry time when waiting for resources
var period = 1 * time.Second

// Maximum time to wait for resources
var timeout = 120 * time.Second

// Options used when deleting resources
var deletePropagation = metav1.DeletePropagationBackground
var gracePeriod = int64(0)

var deleteOpts = []client.DeleteOptionFunc{
	client.GracePeriodSeconds(gracePeriod),
	client.PropagationPolicy(deletePropagation),
}

var InfinispanTypeMeta = metav1.TypeMeta{
	APIVersion: "infinispan.org/v1",
	Kind:       "Infinispan",
}

// Runtime scheme
var scheme = runtime.NewScheme()

var log = logf.Log.WithName("kubernetes_test")

// TestKubernetes abstracts testing related interaction with a Kubernetes cluster
type TestKubernetes struct {
	Kubernetes *util.Kubernetes
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
	ExpectNoError(err)
}

// NewTestKubernetes creates a new instance of TestKubernetes
func NewTestKubernetes() *TestKubernetes {
	mapperProvider := NewDynamicRESTMapper
	kubernetes, err := util.NewKubernetesFromLocalConfig(scheme, mapperProvider)
	ExpectNoError(err)
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
		ExpectNoError(err)
		return
	}

	ExpectNoError(err)
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
	err := k.Kubernetes.Client.Delete(context.TODO(), obj, deleteOpts...)
	ExpectMaybeNotFound(err)

	fmt.Println("Waiting for the namespace to be removed")
	ns := types.NamespacedName{Name: namespace, Namespace: namespace}
	err = wait.Poll(period, timeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), ns, obj)
		if err != nil && errors.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	})
	ExpectNoError(err)
}

// CreateInfinispan creates an Infinispan resource in the given namespace
func (k TestKubernetes) CreateInfinispan(infinispan *ispnv1.Infinispan, namespace string) {
	infinispan.Namespace = namespace
	err := k.Kubernetes.Client.Create(context.TODO(), infinispan)
	ExpectNoError(err)
}

// DeleteInfinispan deletes the infinispan resource
// and waits that all the pods are gone
func (k TestKubernetes) DeleteInfinispan(infinispan *ispnv1.Infinispan, timeout time.Duration) {
	err := k.Kubernetes.Client.Delete(context.TODO(), infinispan, deleteOpts...)
	ExpectNoError(err)

	// TODO getting list of infinispan pods is also done in controller (refactor)
	podList := &v1.PodList{}
	infinispanLabels := map[string]string{"app": "infinispan-pod"}
	labelSelector := labels.SelectorFromSet(infinispanLabels)
	listOps := &client.ListOptions{Namespace: infinispan.Namespace, LabelSelector: labelSelector}
	err = wait.Poll(period, timeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.List(context.TODO(), listOps, podList)
		pods := podList.Items
		if err != nil || len(pods) != 0 {
			return false, nil
		}
		return true, nil
	})
	ExpectNoError(err)
	// Check that PersistentVolumeClaims have been cleanup
	err = wait.Poll(period, timeout, func() (done bool, err error) {
		pvc := &v1.PersistentVolumeClaimList{}
		err = k.Kubernetes.Client.List(context.TODO(), listOps, pvc)
		pvcs := pvc.Items
		if err != nil || len(pvcs) != 0 {
			return false, nil
		}
		return true, nil
	})
	ExpectNoError(err)
}

// GracefulShutdownInfinispan deletes the infinispan resource
// and waits that all the pods are gone
func (k TestKubernetes) GracefulShutdownInfinispan(infinispan *ispnv1.Infinispan, timeout time.Duration) {
	ns := types.NamespacedName{Name: infinispan.Name, Namespace: infinispan.Namespace}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
	ExpectNoError(err)
	infinispan.Spec.Replicas = 0

	// Workaround for OpenShift local test (clear GVK on decode in the client)
	infinispan.TypeMeta = InfinispanTypeMeta
	err = k.Kubernetes.Client.Update(context.TODO(), infinispan)
	ExpectNoError(err)

	err = wait.Poll(period, timeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
		c := infinispan.GetCondition("gracefulShutdown")
		if err != nil || c == nil || *c != "True" {
			return false, nil
		}
		return true, nil
	})

	ExpectNoError(err)
}

// GracefulRestartInfinispan restarts the infinispan resource
// and waits that cluster is wellformed
func (k TestKubernetes) GracefulRestartInfinispan(infinispan *ispnv1.Infinispan, replicas int32, timeout time.Duration) {
	ns := types.NamespacedName{Name: infinispan.Name, Namespace: infinispan.Namespace}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
	ExpectNoError(err)
	infinispan.Spec.Replicas = replicas
	err = wait.Poll(period, timeout, func() (done bool, err error) {
		// Workaround for OpenShift local test (clear GVK on decode in the client)
		infinispan.TypeMeta = InfinispanTypeMeta
		err1 := k.Kubernetes.Client.Update(context.TODO(), infinispan)
		if err1 != nil {
			fmt.Printf("Error updating infinispan %s\n", err1)
			err1 = k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
			infinispan.Spec.Replicas = replicas
			return false, nil
		}
		return true, nil
	})

	ExpectNoError(err)

	err = wait.Poll(period, timeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
		c := infinispan.GetCondition("wellFormed")
		if err != nil || c == nil || *c != "True" {
			return false, nil
		}
		return true, nil
	})

	ExpectNoError(err)
}

// CreateOrUpdateRole creates or updates a Role in the given namespace
func (k TestKubernetes) CreateOrUpdateRole(role *rbacv1.Role, namespace string) {
	ns := types.NamespacedName{Name: role.ObjectMeta.Name, Namespace: namespace}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, role)
	if err != nil && errors.IsNotFound(err) {
		role.Namespace = namespace
		err = k.Kubernetes.Client.Create(context.TODO(), role)
		ExpectNoError(err)
		return
	} else if err == nil {
		err = k.Kubernetes.Client.Update(context.TODO(), role)
		ExpectNoError(err)
		return
	}

	ExpectNoError(err)
}

// CreateOrUpdateSa creates or updates a ServiceAccount in the given namespace
func (k TestKubernetes) CreateOrUpdateSa(sa *v1.ServiceAccount, namespace string) {
	ns := types.NamespacedName{Name: sa.ObjectMeta.Name, Namespace: namespace}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, sa)
	if err != nil && errors.IsNotFound(err) {
		sa.Namespace = namespace
		err = k.Kubernetes.Client.Create(context.TODO(), sa)
		ExpectNoError(err)
		return
	} else if err == nil {
		err = k.Kubernetes.Client.Update(context.TODO(), sa)
		ExpectNoError(err)
		return
	}

	ExpectNoError(err)
}

// CreateOrUpdateRoleBinding creates or updates a RoleBinding in the given namespace
func (k TestKubernetes) CreateOrUpdateRoleBinding(binding *rbacv1.RoleBinding, namespace string) {
	ns := types.NamespacedName{Name: binding.ObjectMeta.Name, Namespace: namespace}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, binding)
	if err != nil && errors.IsNotFound(err) {
		binding.Namespace = namespace
		err = k.Kubernetes.Client.Create(context.TODO(), binding)
		ExpectNoError(err)
		return
	} else if err == nil {
		err = k.Kubernetes.Client.Update(context.TODO(), binding)
		ExpectNoError(err)
		return
	}

	ExpectNoError(err)
}

// CreateAndWaitForCRD creates a Custom Resource Definition, waiting it to become ready.
func (k TestKubernetes) CreateAndWaitForCRD(crd *apiextv1beta1.CustomResourceDefinition, namespace string) {
	fmt.Printf("Create CRD %s\n", crd.ObjectMeta.Name)

	ns := types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: namespace}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, crd)
	if err != nil && errors.IsNotFound(err) {
		err = k.Kubernetes.Client.Create(context.TODO(), crd)
		ExpectNoError(err)
	}

	fmt.Println("Wait for CRD to created")
	err = wait.Poll(period, timeout, func() (done bool, err error) {
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

	ExpectNoError(err)
}

// WaitForExternalService checks if an http server is listening at the endpoint exposed by the service (ns, name)
func (k TestKubernetes) WaitForExternalService(routeName string, timeout time.Duration, client *http.Client, user, password, protocol, namespace string) string {
	var hostAndPort string
	err := wait.Poll(period, timeout, func() (done bool, err error) {
		route := &v1.Service{}
		ns := types.NamespacedName{Name: routeName, Namespace: namespace}
		err = k.Kubernetes.Client.Get(context.TODO(), ns, route)
		ExpectNoError(err)

		if route.Spec.Type == "NodePort" {
			// depending on the k8s cluster setting, service is sometime available
			// via Ingress address sometime via node port. So trying both the methods
			// Try node port first
			hostAndPort = fmt.Sprintf("%s:%d", k.Kubernetes.PublicIP(), k.Kubernetes.GetNodePort(route))
			result := checkExternalAddress(client, user, password, protocol, hostAndPort)
			if result {
				return result, nil
			}

			// if node port fails and it points to 127.0.0.1,
			// it could be running kubernetes-inside-docker,
			// so try using cluster ip instead
			if strings.Contains(hostAndPort, "127.0.0.1") {
				log.Info("Running on kind (kubernetes-inside-docker), try accessing via node port forward")
				hostAndPort = "127.0.0.1:11222"
				result := checkExternalAddress(client, user, password, protocol, hostAndPort)
				if result {
					return result, nil
				}
			}
		}

		// then try to get ingress information
		hostAndPort, err = k.getExternalAddress(route, namespace)
		if err != nil {
			return false, nil
		}
		return checkExternalAddress(client, user, password, protocol, hostAndPort), nil
	})
	ExpectNoError(err)
	return hostAndPort
}

func checkExternalAddress(client *http.Client, user, password, protocol, hostAndPort string) bool {
	httpURL := fmt.Sprintf("%s://%s/%s", protocol, hostAndPort, util.ServerHTTPHealthPath)
	req, err := http.NewRequest("GET", httpURL, nil)
	ExpectNoError(err)
	req.SetBasicAuth(user, password)
	resp, err := client.Do(req)
	if err != nil {
		log.Error(err, "Unable to complete HTTP request for external address")
		return false
	}
	log.Info("Received response for external address", "response code", resp.StatusCode)
	return resp.StatusCode == http.StatusOK
}

func (k TestKubernetes) getExternalAddress(route *v1.Service, namespace string) (string, error) {
	// If the cluster exposes external IP then return it
	if len(route.Status.LoadBalancer.Ingress) == 1 {
		if route.Status.LoadBalancer.Ingress[0].IP != "" {
			return route.Status.LoadBalancer.Ingress[0].IP + ":11222", nil
		}
		if route.Status.LoadBalancer.Ingress[0].Hostname != "" {
			return route.Status.LoadBalancer.Ingress[0].Hostname + ":11222", nil
		}
	}

	// Return empty address if nothing available
	return "", fmt.Errorf("external address not found")
}

// WaitForPods waits for pods in the given namespace, having a certain label to reach the desired count in ContainersReady state
func (k TestKubernetes) WaitForPods(label string, required int, timeout time.Duration, namespace string) {
	labelSelector, err := labels.Parse(label)
	ExpectNoError(err)

	listOps := &client.ListOptions{Namespace: namespace, LabelSelector: labelSelector}
	podList := &v1.PodList{}
	err = wait.Poll(period, timeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.List(context.TODO(), listOps, podList)
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

	ExpectNoError(err)
}

func debugPods(required int, pods []v1.Pod) {
	log.Info("pod list incomplete", "required", required, "pod list size", len(pods))
	for _, pod := range pods {
		log.Info("pod info", "name", pod.ObjectMeta.Name, "statuses", pod.Status.ContainerStatuses)
	}
}

// DeleteCRD deletes a CustomResourceDefinition
func (k TestKubernetes) DeleteCRD(name string) {
	crd := &apiextv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	err := k.Kubernetes.Client.Delete(context.TODO(), crd, deleteOpts...)
	ExpectMaybeNotFound(err)
}

// Nodes returns a list of running nodes in the cluster
func (k TestKubernetes) Nodes() []string {
	nodes := &v1.NodeList{}
	listOps := &client.ListOptions{}
	err := k.Kubernetes.Client.List(context.TODO(), listOps, nodes)
	ExpectNoError(err)

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
	ExpectNoError(err)

	listOps := &client.ListOptions{Namespace: namespace, LabelSelector: labelSelector}
	pods := &v1.PodList{}
	err = k.Kubernetes.Client.List(context.TODO(), listOps, pods)
	ExpectNoError(err)

	return pods.Items
}

// CreateSecret creates a secret
func (k TestKubernetes) CreateSecret(secret *v1.Secret, namespace string) {
	secret.Namespace = namespace
	err := k.Kubernetes.Client.Create(context.TODO(), secret)
	ExpectNoError(err)
}

// DeleteSecret deletes a secret
func (k TestKubernetes) DeleteSecret(secret *v1.Secret) {
	err := k.Kubernetes.Client.Delete(context.TODO(), secret, deleteOpts...)
	ExpectNoError(err)
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
	filename := getAbsolutePath("../../deploy/rbac.yaml")
	f, err := os.Open(filename)
	ExpectNoError(err)
	yamlReader := yaml.NewYAMLReader(bufio.NewReader(f))

	read, _ := yamlReader.Read()
	role := rbacv1.Role{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read))).Decode(&role)
	ExpectNoError(err)
	k.CreateOrUpdateRole(&role, namespace)

	read2, _ := yamlReader.Read()
	sa := v1.ServiceAccount{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read2))).Decode(&sa)
	ExpectNoError(err)
	k.CreateOrUpdateSa(&sa, namespace)

	read3, _ := yamlReader.Read()
	binding := rbacv1.RoleBinding{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read3))).Decode(&binding)
	ExpectNoError(err)
	k.CreateOrUpdateRoleBinding(&binding, namespace)
}

// Obtain the file absolute path given a relative path
func getAbsolutePath(relativeFilePath string) string {
	if !strings.HasPrefix(relativeFilePath, ".") {
		return relativeFilePath
	}
	dir, _ := os.Getwd()
	absPath, _ := filepath.Abs(dir + "/" + relativeFilePath)
	return absPath
}

func (k TestKubernetes) installCRD(namespace string) {
	filename := getAbsolutePath("../../deploy/crd.yaml")
	f, err := os.Open(filename)
	ExpectNoError(err)
	yamlReader := yaml.NewYAMLReader(bufio.NewReader(f))

	read, _ := yamlReader.Read()
	crd := apiextv1beta1.CustomResourceDefinition{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read))).Decode(&crd)
	ExpectNoError(err)
	k.CreateAndWaitForCRD(&crd, namespace)
}

// Run the operator locally
func runOperatorLocally(stopCh chan struct{}, namespace string) {
	kubeConfig := util.FindKubeConfig()
	_ = os.Setenv("WATCH_NAMESPACE", namespace)
	_ = os.Setenv("KUBECONFIG", kubeConfig)
	launcher.Launch(launcher.Parameters{StopChannel: stopCh})
}
