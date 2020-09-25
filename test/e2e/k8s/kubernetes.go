package util

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnctrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/launcher"
	tconst "github.com/infinispan/infinispan-operator/test/e2e/constants"
	"github.com/infinispan/infinispan-operator/test/e2e/utils"
	testutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
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
	Kubernetes *kube.Kubernetes
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
func NewTestKubernetes(ctx string) *TestKubernetes {
	mapperProvider := apiutil.NewDynamicRESTMapper
	kubernetes, err := kube.NewKubernetesFromLocalConfig(scheme, mapperProvider, ctx)
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

	err := k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: namespace}, obj)
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
	err := k.Kubernetes.Client.Delete(context.TODO(), obj, tconst.DeleteOpts...)
	utils.ExpectMaybeNotFound(err)

	fmt.Println("Waiting for the namespace to be removed")
	err = wait.Poll(tconst.DefaultPollPeriod, tconst.MaxWaitTimeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: namespace}, obj)
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
	err := k.Kubernetes.Client.Delete(context.TODO(), infinispan, tconst.DeleteOpts...)
	utils.ExpectNoError(err)

	// TODO getting list of infinispan resources is also done in controller (refactor)
	podList := &v1.PodList{}
	labelSelector := labels.SelectorFromSet(ispnctrl.PodLabels(infinispan.Name))
	listOps := &client.ListOptions{Namespace: infinispan.Namespace, LabelSelector: labelSelector}
	err = wait.Poll(tconst.DefaultPollPeriod, timeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.List(context.TODO(), podList, listOps)
		pods := podList.Items
		if err != nil || len(pods) != 0 {
			return false, nil
		}
		return true, nil
	})
	utils.ExpectNoError(err)
	// Check that PersistentVolumeClaims have been cleanup
	err = wait.Poll(tconst.DefaultPollPeriod, timeout, func() (done bool, err error) {
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
	ns := types.NamespacedName{Namespace: infinispan.Namespace, Name: infinispan.Name}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
	utils.ExpectNoError(err)
	infinispan.Spec.Replicas = 0

	// Workaround for OpenShift local test (clear GVK on decode in the client)
	infinispan.TypeMeta = tconst.InfinispanTypeMeta
	err = k.Kubernetes.Client.Update(context.TODO(), infinispan)
	utils.ExpectNoError(err)

	err = wait.Poll(tconst.DefaultPollPeriod, timeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
		c := infinispan.GetCondition("gracefulShutdown")
		if err != nil || c == nil || *c != metav1.ConditionTrue {
			return false, nil
		}
		return true, nil
	})

	utils.ExpectNoError(err)
}

// GracefulRestartInfinispan restarts the infinispan resource
// and waits that cluster is wellformed
func (k TestKubernetes) GracefulRestartInfinispan(infinispan *ispnv1.Infinispan, replicas int32, timeout time.Duration) {
	ns := types.NamespacedName{Namespace: infinispan.Namespace, Name: infinispan.Name}
	err := k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
	utils.ExpectNoError(err)
	infinispan.Spec.Replicas = replicas
	err = wait.Poll(tconst.DefaultPollPeriod, timeout, func() (done bool, err error) {
		// Workaround for OpenShift local test (clear GVK on decode in the client)
		infinispan.TypeMeta = tconst.InfinispanTypeMeta
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

	err = wait.Poll(tconst.DefaultPollPeriod, timeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
		c := infinispan.GetCondition("wellFormed")
		if err != nil || c == nil || *c != metav1.ConditionTrue {
			return false, nil
		}
		return true, nil
	})

	utils.ExpectNoError(err)
}

// CreateAndWaitForCRD creates a Custom Resource Definition, waiting it to become ready.
func (k TestKubernetes) CreateAndWaitForCRD(crd *apiextv1beta1.CustomResourceDefinition) {
	fmt.Printf("Create CRD %s\n", crd.Name)

	err := k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: crd.Name}, crd)
	if err != nil && errors.IsNotFound(err) {
		err = k.Kubernetes.Client.Create(context.TODO(), crd)
		utils.ExpectNoError(err)
	}

	fmt.Println("Wait for CRD to created")
	err = wait.Poll(tconst.DefaultPollPeriod, tconst.MaxWaitTimeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: crd.Name}, crd)
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
func (k TestKubernetes) WaitForExternalService(routeName string, timeout time.Duration, client *http.Client, user, password, protocol, namespace string) string {
	var hostAndPort string
	err := wait.Poll(tconst.DefaultPollPeriod, timeout, func() (done bool, err error) {
		route := &v1.Service{}
		err = k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: routeName}, route)
		utils.ExpectNoError(err)

		// depending on the k8s cluster setting, service is sometime available
		// via Ingress address sometime via node port. So trying both the methods
		// Try node port first
		hosts := k.Kubernetes.GetNodesHost()
		if len(hosts) > 0 {
			// It should be ok to use the first node address available
			hostAndPort = fmt.Sprintf("%s:%d", hosts[0], k.Kubernetes.GetNodePort(route))
			result := checkExternalAddress(client, user, password, protocol, hostAndPort)
			if result {
				return result, nil
			}
		}

		// if node port fails and it points to 127.0.0.1,
		// it could be running kubernetes-inside-docker,
		// so try using cluster ip instead
		if strings.Contains(hostAndPort, "127.0.0.1") {
			log.Info("Running on kind (kubernetes-inside-docker), try accessing via node port forward")
			hostAndPort = fmt.Sprintf("127.0.0.1:%d", tconst.InfinispanPort)
			result := checkExternalAddress(client, user, password, protocol, hostAndPort)
			if result {
				return result, nil
			}
		}

		// then try to get ingress information
		hostAndPort, err = k.getExternalAddress(route)
		if err != nil {
			return false, nil
		}
		return checkExternalAddress(client, user, password, protocol, hostAndPort), nil
	})
	utils.ExpectNoError(err)
	return hostAndPort
}

func checkExternalAddress(client *http.Client, user, password, protocol, hostAndPort string) bool {
	httpURL := fmt.Sprintf("%s://%s/%s", protocol, hostAndPort, consts.ServerHTTPHealthPath)
	req, err := http.NewRequest("GET", httpURL, nil)
	utils.ExpectNoError(err)
	req.SetBasicAuth(user, password)
	resp, err := client.Do(req)
	if err != nil {
		log.Error(err, "Unable to complete HTTP request for external address")
		return false
	}
	log.Info("Received response for external address", "response code", resp.StatusCode)
	return resp.StatusCode == http.StatusOK
}

func (k TestKubernetes) getExternalAddress(route *v1.Service) (string, error) {
	// If the cluster exposes external IP then return it
	if len(route.Status.LoadBalancer.Ingress) == 1 {
		if route.Status.LoadBalancer.Ingress[0].IP != "" {
			return fmt.Sprintf(route.Status.LoadBalancer.Ingress[0].IP+":%d", tconst.InfinispanPort), nil
		}
		if route.Status.LoadBalancer.Ingress[0].Hostname != "" {
			return fmt.Sprintf(route.Status.LoadBalancer.Ingress[0].Hostname+":%d", tconst.InfinispanPort), nil
		}
	}

	// Return empty address if nothing available
	return "", fmt.Errorf("external address not found")
}

// WaitForPods waits for pods in the given namespace, having a certain label to reach the desired count in ContainersReady state
func (k TestKubernetes) WaitForPods(required int, timeout time.Duration, clusterName, namespace string) {
	labelSelector := labels.SelectorFromSet(ispnctrl.PodLabels(clusterName))

	listOps := &client.ListOptions{Namespace: namespace, LabelSelector: labelSelector}
	podList := &v1.PodList{}
	err := wait.Poll(tconst.DefaultPollPeriod, timeout, func() (done bool, err error) {
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
				if (cond.Type) == v1.ContainersReady && cond.Status == v1.ConditionTrue {
					podReady = true
				}
			}
			allReady = allReady && podReady
		}
		return allReady, nil
	})

	utils.ExpectNoError(err)
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
	err := k.Kubernetes.Client.Delete(context.TODO(), crd, tconst.DeleteOpts...)
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
func (k TestKubernetes) GetPods(clusterName, namespace string) []v1.Pod {
	// TODO getting list of infinispan resources is also done in controller (refactor)
	labelSelector := labels.SelectorFromSet(ispnctrl.PodLabels(clusterName))
	listOps := &client.ListOptions{Namespace: namespace, LabelSelector: labelSelector}
	pods := &v1.PodList{}
	err := k.Kubernetes.Client.List(context.TODO(), pods, listOps)
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
	err := k.Kubernetes.Client.Delete(context.TODO(), secret, tconst.DeleteOpts...)
	utils.ExpectNoError(err)
}

// RunOperator runs an operator on a Kubernetes cluster
func (k TestKubernetes) RunOperator(namespace string) chan struct{} {
	k.installCRD()
	stopCh := make(chan struct{})
	go runOperatorLocally(stopCh, namespace)
	return stopCh
}

func (k TestKubernetes) installCRD() {
	yamlReader, err := testutils.GetYamlReaderFromFile("../../deploy/crds/infinispan.org_infinispans_crd.yaml")
	read, _ := yamlReader.Read()
	crdInfinispan := apiextv1beta1.CustomResourceDefinition{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read))).Decode(&crdInfinispan)
	utils.ExpectNoError(err)
	k.CreateAndWaitForCRD(&crdInfinispan)

	yamlReader, err = testutils.GetYamlReaderFromFile("../../deploy/crds/infinispan.org_caches_crd.yaml")
	read, _ = yamlReader.Read()
	crdCache := apiextv1beta1.CustomResourceDefinition{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read))).Decode(&crdCache)
	utils.ExpectNoError(err)
	k.CreateAndWaitForCRD(&crdCache)
}

// Run the operator locally
func runOperatorLocally(stopCh chan struct{}, namespace string) {
	kubeConfig := kube.FindKubeConfig()
	_ = os.Setenv("WATCH_NAMESPACE", namespace)
	_ = os.Setenv("KUBECONFIG", kubeConfig)
	launcher.Launch(launcher.Parameters{StopChannel: stopCh})
}
