package okd

import (
	"errors"
	"io/ioutil"
	"net/http"
	"path"
	"strconv"

	api "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/generated/clientset/versioned/typed/infinispan/v1"
	authv1 "github.com/openshift/api/authorization/v1"
	routev1 "github.com/openshift/api/route/v1"
	authclient "github.com/openshift/client-go/authorization/clientset/versioned/typed/authorization/v1"
	routeclient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	userclient "github.com/openshift/client-go/user/clientset/versioned/typed/user/v1"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"reflect"
	"time"

	v1 "k8s.io/api/core/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	apiv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"

	appsV1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	coreV1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"bytes"

	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

// ExternalOKD provides a simplified client for a running OKD cluster
type ExternalOKD struct {
	coreClient  *coreV1.CoreV1Client
	userClient  *userclient.UserV1Client
	routeClient *routeclient.RouteV1Client
	authClient  *authclient.AuthorizationV1Client
	appsClient  *appsV1.AppsV1Client
	extensions  *apiextclient.ApiextensionsV1beta1Client
	ispnClient  *ispnv1.InfinispanV1Client
	restConfig  *restclient.Config
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

// constructs a new ExternalOKD client providing the path of kubeconfig file.
func NewOKDClient(kubeConfigLocation string) *ExternalOKD {
	c := new(ExternalOKD)
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
	coreClient, err := coreV1.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	c.coreClient = coreClient

	userV1Client, err := userclient.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	c.userClient = userV1Client

	routeV1Client, err := routeclient.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	c.routeClient = routeV1Client

	authV1Client, err := authclient.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	c.authClient = authV1Client

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

	return c
}

// CoreClient returns a ready to use core client
func (c ExternalOKD) CoreClient() *coreV1.CoreV1Client {
	return c.coreClient
}

// Creates a Custom Resource Definition, waiting it to become ready.
func (c ExternalOKD) CreateAndWaitForCRD(crd *apiextv1beta1.CustomResourceDefinition) {
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
func (c ExternalOKD) CreateInfinispan(infinispan *api.Infinispan, namespace string) {
	_, e := c.ispnClient.Infinispans(namespace).Create(infinispan)
	if e != nil {
		panic(e)
	}
}

// DeleteInfinispan deletes the infinispan resource
// and waits that all the pods are gone
func (c ExternalOKD) DeleteInfinispan(name string, ns string, label string, timeout time.Duration) error {
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
func (c ExternalOKD) CreateOrUpdateConfigMap(name string, filePath string, namespace string) {
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
func (c ExternalOKD) CreateOrUpdateRole(role *authv1.Role, namespace string) {
	existing, _ := c.authClient.Roles(namespace).Get(role.ObjectMeta.Name, metaV1.GetOptions{})

	if reflect.DeepEqual(authv1.Role{}, *existing) { // is empty
		_, e := c.authClient.Roles(namespace).Create(role)
		if e != nil {
			panic(e)
		}
	} else {
		_, e := c.authClient.Roles(namespace).Update(role)
		if e != nil {
			panic(e)
		}
	}
}

// Creates or updates a RoleBinding in the given namespace
func (c ExternalOKD) CreateOrUpdateRoleBinding(binding *authv1.RoleBinding, namespace string) {
	bindingsSvc := c.authClient.RoleBindings(namespace)

	existing, _ := bindingsSvc.Get(binding.Name, metaV1.GetOptions{})

	if reflect.DeepEqual(authv1.RoleBinding{}, *existing) { // is empty
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
func (c ExternalOKD) CreateOrUpdateSa(sa *v1.ServiceAccount, namespace string) {
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
func (c ExternalOKD) DeleteProject(name string) {
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
func (c ExternalOKD) DeleteCRD(name string) {
	e := c.extensions.CustomResourceDefinitions().Delete(name, &deleteOpts)
	if e != nil {
		panic(e)
	}
}

func (ExternalOKD) LoginAs(user string, pass string) {
	panic("implement me")
}

func (c ExternalOKD) LoginAsSystem() {
	panic("implement me")
}

func (c ExternalOKD) NewProject(projectName string) {
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
func (c ExternalOKD) Nodes() []string {
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

func (c ExternalOKD) Pods(namespace, label string) []string {
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

// ExecuteCmdOnPod Excecutes command on pod
// commands array example { "/usr/bin/ls", "folderName" }
// execIn, execOut, execErr stdin, stdout, stderr stream for the command
func (c ExternalOKD) ExecuteCmdOnPod(namespace, podName string, commands []string,
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
func (c ExternalOKD) WaitForPods(ns, label string, required int, timeout time.Duration) error {
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
func (c ExternalOKD) GetPods(ns, label string) ([]v1.Pod, error) {
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

func (c ExternalOKD) WaitForRoute(url string, client *http.Client, timeout time.Duration, user string, pass string) {
	err := wait.Poll(period, timeout, func() (done bool, err error) {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return false, err
		}
		req.SetBasicAuth(user, pass)
		resp, err := client.Do(req)
		if err != nil {
			return false, err
		}
		if resp.StatusCode == http.StatusNotFound {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		panic(err.Error())
	}
}

// Return the currently authenticated user name
func (c ExternalOKD) WhoAmI() string {
	user, e := c.userClient.Users().Get("~", metaV1.GetOptions{})
	if e != nil {
		panic(e)
	}

	return user.Name
}

func (c ExternalOKD) CreateRoute(ns, serviceName string, portString string) *routev1.Route {
	route := &routev1.Route{
		TypeMeta: metaV1.TypeMeta{APIVersion: routev1.SchemeGroupVersion.String(), Kind: "Route"},
		ObjectMeta: metaV1.ObjectMeta{
			Name: serviceName,
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Name: serviceName,
			},
			Port: resolveRoutePort(portString),
		},
	}

	router, err := c.routeClient.Routes(ns).Create(route)
	if err != nil {
		panic(err.Error())
	}

	return router
}

func (c ExternalOKD) DeleteRoute(routeName string, namespace string) {
	err := c.routeClient.Routes(namespace).Delete(routeName, &deleteOpts)
	if err != nil {
		panic(err)
	}
}

func resolveRoutePort(portString string) *routev1.RoutePort {
	if len(portString) == 0 {
		return nil
	}
	var routePort intstr.IntOrString
	integer, err := strconv.Atoi(portString)
	if err != nil {
		routePort = intstr.FromString(portString)
	} else {
		routePort = intstr.FromInt(integer)
	}
	return &routev1.RoutePort{
		TargetPort: routePort,
	}
}
