package k8s

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"

	api "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/generated/clientset/versioned/typed/infinispan/v1"
	"k8s.io/client-go/kubernetes/scheme"
	rbacclient "k8s.io/client-go/kubernetes/typed/rbac/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	apiv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"

	appsV1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"

	"bytes"

	ispnutil "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

// ExternalK8s provides a simplified client for a running OKD cluster
type ExternalK8s struct {
	coreClient    *coreclient.CoreV1Client
	rbacClient    *rbacclient.RbacV1Client
	appsClient    *appsV1.AppsV1Client
	extensions    *apiextclient.ApiextensionsV1beta1Client
	ispnClient    *ispnv1.InfinispanV1Client
	restConfig    *restclient.Config
	ispnCliHelper *ispnutil.IspnCliHelper
}

var log = logf.Log.WithName("main_test")

// Options used when deleting resources
var deletePropagation = apiv1.DeletePropagationBackground
var gracePeriod = int64(0)
var deleteOpts = apiv1.DeleteOptions{PropagationPolicy: &deletePropagation, GracePeriodSeconds: &gracePeriod}

// Default retry time when waiting for resources
var period = 1 * time.Second

// Maximum time to wait for resources
var timeout = 120 * time.Second

// constructs a new ExternalK8s client providing the path of kubeconfig file.
func NewK8sClient(kubeConfigLocation string) *ExternalK8s {
	c := new(ExternalK8s)
	dir := GetAbsolutePath(kubeConfigLocation)
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: dir},
		&clientcmd.ConfigOverrides{})

	config, err := clientConfig.ClientConfig()
	c.restConfig = config
	if err != nil {
		panic(err.Error())
	}

	// Initialize clients for different kinds of resources
	coreClient, err := coreclient.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	c.coreClient = coreClient

	rbacV1Client, err := rbacclient.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	c.rbacClient = rbacV1Client

	appsV1Client, err := appsV1.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	c.appsClient = appsV1Client

	client, err := apiextclient.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	c.extensions = client

	ispnClient, err := ispnv1.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	c.ispnClient = ispnClient

	c.ispnCliHelper = ispnutil.NewIspnCliHelper()

	return c
}

// CoreClient returns a ready to use core client
func (c ExternalK8s) CoreClient() *coreclient.CoreV1Client {
	return c.coreClient
}

// Creates a Custom Resource Definition, waiting it to become ready.
func (c ExternalK8s) CreateAndWaitForCRD(crd *apiextv1beta1.CustomResourceDefinition) {
	customResourceSvc := c.extensions.CustomResourceDefinitions()

	existing, _ := customResourceSvc.Get(crd.Name, metaV1.GetOptions{})

	if reflect.DeepEqual(apiextv1beta1.CustomResourceDefinition{}, *existing) {
		_, e := customResourceSvc.Create(crd)
		if e != nil {
			panic(e)
		}
	}

	err := wait.Poll(period, timeout, func() (done bool, err error) {
		crd, err := customResourceSvc.Get(crd.Name, metaV1.GetOptions{})
		conditions := crd.Status.Conditions
		established := false
		for _, cond := range conditions {
			if cond.Type == "Established" {
				established = cond.Status == "True"
			}
		}
		return established, nil
	})

	if err != nil {
		panic(err)
	}

}

// Creates an Infinispan resource in the given namespace
func (c ExternalK8s) CreateInfinispan(infinispan *api.Infinispan, namespace string) {
	_, e := c.ispnClient.Infinispans(namespace).Create(infinispan)
	if e != nil {
		panic(e)
	}
}

// DeleteInfinispan deletes the infinispan resource
// and waits that all the pods are gone
func (c ExternalK8s) DeleteInfinispan(name string, ns string, label string, timeout time.Duration) error {
	ispnSvc := c.ispnClient.Infinispans(ns)
	e := ispnSvc.Delete(name, &deleteOpts)
	if e != nil {
		panic(e)
	}
	podSvc := c.coreClient.Pods(ns)
	err := wait.Poll(period, timeout, func() (done bool, err error) {
		podList, err := podSvc.List(metaV1.ListOptions{
			LabelSelector: label,
		})
		pods := podList.Items
		if err != nil || len(pods) != 0 {
			return false, nil
		}
		return true, nil
	})
	return err
}

// Creates or updates a config map based on a file
func (c ExternalK8s) CreateOrUpdateConfigMap(name string, filePath string, namespace string) {
	bytes, e := ioutil.ReadFile(filePath)
	if e != nil {
		panic(errors.New("Cannot read file " + filePath))
	}
	_, file := path.Split(filePath)

	configMap := &v1.ConfigMap{
		ObjectMeta: metaV1.ObjectMeta{
			Name: name,
		},
		Data: map[string]string{
			file: string(bytes),
		},
	}

	cm, e := c.coreClient.ConfigMaps(namespace).Get(name, metaV1.GetOptions{})

	if reflect.DeepEqual(v1.ConfigMap{}, *cm) { // empty result
		_, e = c.coreClient.ConfigMaps(namespace).Create(configMap)
	} else {
		_, e = c.coreClient.ConfigMaps(namespace).Update(configMap)
	}

	if e != nil {
		panic(e)
	}
}

// Creates or updates a Role in the given namespace
func (c ExternalK8s) CreateOrUpdateRole(role *rbacv1.Role, namespace string) {
	existing, _ := c.rbacClient.Roles(namespace).Get(role.ObjectMeta.Name, metaV1.GetOptions{})

	if reflect.DeepEqual(rbacv1.Role{}, *existing) { // is empty
		_, e := c.rbacClient.Roles(namespace).Create(role)
		if e != nil {
			panic(e)
		}
	} else {
		_, e := c.rbacClient.Roles(namespace).Update(role)
		if e != nil {
			panic(e)
		}
	}
}

// Creates or updates a RoleBinding in the given namespace
func (c ExternalK8s) CreateOrUpdateRoleBinding(binding *rbacv1.RoleBinding, namespace string) {
	bindingsSvc := c.rbacClient.RoleBindings(namespace)

	existing, _ := bindingsSvc.Get(binding.Name, metaV1.GetOptions{})

	if reflect.DeepEqual(rbacv1.RoleBinding{}, *existing) { // is empty
		_, e := bindingsSvc.Create(binding)
		if e != nil {
			panic(e)
		}
	} else {
		_, e := bindingsSvc.Update(binding)
		if e != nil {
			panic(e)
		}
	}
}

// Creates or updates a ServiceAccount in the given namespace
func (c ExternalK8s) CreateOrUpdateSa(sa *v1.ServiceAccount, namespace string) {
	accountsSvc := c.coreClient.ServiceAccounts(namespace)
	existing, _ := accountsSvc.Get(sa.Name, metaV1.GetOptions{})

	if reflect.DeepEqual(v1.ServiceAccount{}, *existing) {
		_, e := accountsSvc.Create(sa)
		if e != nil {
			panic(e)
		}
	} else {
		_, e := accountsSvc.Update(sa)
		if e != nil {
			panic(e)
		}
	}
}

// Delete the Custom Resource Definition and dependent resources
func (c ExternalK8s) DeleteProject(name string) {
	projectSvc := c.coreClient.Namespaces()

	_ = projectSvc.Delete(name, &deleteOpts)

	// Wait until deleted
	err := wait.Poll(period, timeout, func() (done bool, err error) {
		project, err := projectSvc.Get(name, metaV1.GetOptions{})

		if reflect.DeepEqual(v1.Namespace{}, *project) { // Non existent
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		panic(err)
	}
}

// Delete the Custom Resource Definition and dependent resources
func (c ExternalK8s) DeleteCRD(name string) {
	e := c.extensions.CustomResourceDefinitions().Delete(name, &deleteOpts)
	if e != nil {
		panic(e)
	}
}

func (ExternalK8s) LoginAs(user string, pass string) {
	panic("implement me")
}

func (c ExternalK8s) LoginAsSystem() {
	panic("implement me")
}

func (c ExternalK8s) NewProject(projectName string) {
	projectSvc := c.coreClient.Namespaces()

	namespace := v1.Namespace{
		ObjectMeta: metaV1.ObjectMeta{
			Name: projectName,
		},
	}

	existing, _ := projectSvc.Get(projectName, metaV1.GetOptions{})

	if reflect.DeepEqual(v1.Namespace{}, *existing) { // Non existent
		_, e := projectSvc.Create(&namespace)
		if e != nil {
			panic(e)
		}
	}
}

// Returns a list of running nodes in the cluster
func (c ExternalK8s) Nodes() []string {
	var s []string
	nodes := c.coreClient.Nodes()
	nodeList, e := nodes.List(metaV1.ListOptions{})
	if e != nil {
		panic(e.Error())
	}
	if nodeList.Size() == 0 {
		return s
	}
	for _, element := range nodeList.Items {
		s = append(s, element.Name)
	}
	return s
}

func (c ExternalK8s) Pods(namespace, label string) []string {
	var s []string
	pods := c.coreClient.Pods(namespace)
	podList, e := pods.List(metaV1.ListOptions{
		LabelSelector: label,
	})
	if e != nil {
		panic(e.Error())
	}
	for _, element := range podList.Items {
		s = append(s, element.Name)
	}
	return s
}

func (c ExternalK8s) PublicIp() string {
	u, err := url.Parse(c.restConfig.Host)
	if err != nil {
		panic(err.Error())
	}
	return u.Hostname()
}

// ExecuteCmdOnPod Excecutes command on pod
// commands array example { "/usr/bin/ls", "folderName" }
// execIn, execOut, execErr stdin, stdout, stderr stream for the command
func (c ExternalK8s) ExecuteCmdOnPod(namespace, podName string, commands []string,
	execIn, execOut, execErr *bytes.Buffer) error {
	// Create a POST request
	execRequest := c.coreClient.RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&v1.PodExecOptions{
			Container: "infinispan",
			Command:   commands,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)
	// Create an executor
	exec, err := remotecommand.NewSPDYExecutor(c.restConfig, "POST", execRequest.URL())
	if err != nil {
		return err
	}
	// Run the command
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  execIn,
		Stdout: execOut,
		Stderr: execErr,
		Tty:    false,
	})
	return err
}

// Waits for pods in the given namespace, having a certain label to reach the desired count in ContainersReady state.
func (c ExternalK8s) WaitForPods(ns, label string, required int, timeout time.Duration) error {
	podSvc := c.coreClient.Pods(ns)
	err := wait.Poll(period, timeout, func() (done bool, err error) {
		podList, err := podSvc.List(metaV1.ListOptions{
			LabelSelector: label,
		})
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
	if err != nil {
		return err
	}
	return nil
}

// GetPods return an array of pods matching the selector label in the given namespace
func (c ExternalK8s) GetPods(ns, label string) ([]v1.Pod, error) {
	podSvc := c.coreClient.Pods(ns)
	podList, err := podSvc.List(metaV1.ListOptions{
		LabelSelector: label,
	})
	if err != nil {
		return nil, err
	}
	return podList.Items, nil
}

func debugPods(required int, pods []v1.Pod) {
	log.Info("pod list incomplete", "required", required, "pod list size", len(pods))
	for _, pod := range pods {
		log.Info("pod info", "name", pod.ObjectMeta.Name, "statuses", pod.Status.ContainerStatuses)
	}
}

// WaitForRoute checks if an http server is listening at the endpoint exposed by the service (ns, name)
func (c ExternalK8s) WaitForRoute(client *http.Client, ns string, name string, timeout time.Duration, user string, pass string) string {
	var hostAndPort string
	err := wait.Poll(period, timeout, func() (done bool, err error) {
		// depending on the c8s cluster setting, service is sometime available
		// via Ingress address sometime via node port. So trying both the methods
		// Try node port first
		hostAndPort = getNodePortAddress(c, ns, name)
		result := checkExternalAddress(client, hostAndPort, ns, name, user, pass)
		if result {
			return result, nil
		}
		// then try ingress
		hostAndPort, err = getExternalAddress(c, ns, name)
		if err != nil {
			return false, nil
		}
		return checkExternalAddress(client, hostAndPort, ns, name, user, pass), nil
	})
	if err != nil {
		panic(err.Error())
	}
	return hostAndPort
}

func checkExternalAddress(client *http.Client, hostAndPort, ns, name, user, pass string) bool {
	req, err := http.NewRequest("GET", "http://"+hostAndPort+"/rest/default/test", nil)
	if err != nil {
		// If we can't create a request just panic
		panic(err.Error())
	}
	req.SetBasicAuth(user, pass)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized {
		return true
	}
	return false
}

// CreateRoute exposes the port 8080 of the Infinispan cluster clusterName
// It return the public address a string: the IP address if available else the hostname
func (c ExternalK8s) CreateRoute(ns, clusterName string, portString string) {
	svc, err := c.coreClient.Services(ns).Get(clusterName, metaV1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}

	// An external route can be simply achieved with a LoadBalancer
	// that has same selectors as original service.
	route := &v1.Service{
		TypeMeta: metaV1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metaV1.ObjectMeta{
			Name:      clusterName + "-" + portString,
			Namespace: ns,
		},
		Spec: v1.ServiceSpec{
			Type:     v1.ServiceTypeLoadBalancer,
			Selector: svc.Spec.Selector,
			Ports: []corev1.ServicePort{
				{
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}
	_, err = c.coreClient.Services(ns).Create(route)
	if err != nil {
		panic(err.Error())
	}
}

func getExternalAddress(c ExternalK8s, ns string, name string) (string, error) {
	router, err := c.coreClient.Services(ns).Get(name, metaV1.GetOptions{})
	if err != nil {
		return "", err
	}
	// If the cluster exposes external IP then return it
	if len(router.Status.LoadBalancer.Ingress) == 1 {
		if router.Status.LoadBalancer.Ingress[0].IP != "" {
			return router.Status.LoadBalancer.Ingress[0].IP + ":8080", nil
		}
		if router.Status.LoadBalancer.Ingress[0].Hostname != "" {
			return router.Status.LoadBalancer.Ingress[0].Hostname + ":8080", nil
		}
	}
	// Return empty address if nothing available
	return "", errors.New("External address not found")
}

func getNodePortAddress(c ExternalK8s, ns string, name string) string {
	router, err := c.coreClient.Services(ns).Get(name, metaV1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}
	return c.PublicIp() + ":" + fmt.Sprint(router.Spec.Ports[0].NodePort)
}

// DeleteRoute delete a service
func (c ExternalK8s) DeleteRoute(ns, serviceName string) {
	err := c.coreClient.Services(ns).Delete(serviceName, &deleteOpts)
	if err != nil {
		panic(err)
	}
}

// GetClusterSize returns the # of cluster members
func (c ExternalK8s) GetClusterSize(namespace, namePod string) (int, error) {
	return c.ispnCliHelper.GetClusterSize(namespace, namePod)
}

func (c ExternalK8s) GetSecret(ns, field string, name string) string {
	secret, err := c.coreClient.Secrets(ns).Get(name, metaV1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}
	return string(secret.Data[field])
}
