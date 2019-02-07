package util

import (
	"errors"
	api "github.com/jboss-dockerfiles/infinispan-server-operator/pkg/apis/infinispan/v1"
	ispnv1 "github.com/jboss-dockerfiles/infinispan-server-operator/pkg/generated/clientset/versioned/typed/infinispan/v1"
	osv1 "github.com/openshift/api/authorization/v1"
	authV1 "github.com/openshift/client-go/authorization/clientset/versioned/typed/authorization/v1"
	userV1 "github.com/openshift/client-go/user/clientset/versioned/typed/user/v1"
	"io/ioutil"
	"k8s.io/client-go/tools/clientcmd"
	"path"

	"k8s.io/api/core/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	apiv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"reflect"
	"time"

	appsV1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	coreV1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

// ExternalOKD provides a simplified client for a running OKD cluster
type ExternalOKD struct {
	coreClient *coreV1.CoreV1Client
	userClient *userV1.UserV1Client
	authClient *authV1.AuthorizationV1Client
	appsClient *appsV1.AppsV1Client
	extensions *apiextclient.ApiextensionsV1beta1Client
	ispnClient *ispnv1.InfinispanV1Client
}

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

	if err != nil {
		panic(err.Error())
	}

	// Initialize clients for different kinds of resources
	coreClient, err := coreV1.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	c.coreClient = coreClient

	userV1Client, err := userV1.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	c.userClient = userV1Client

	authV1Client, err := authV1.NewForConfig(config)
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

// Deletes an Infinispan resource in the given namespace
func (c ExternalOKD) DeleteInfinispan(name string, namespace string) {
	ispnSvc := c.ispnClient.Infinispans(namespace)
	e := ispnSvc.Delete(name, &deleteOpts)
	if e != nil {
		panic(e)
	}
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
func (c ExternalOKD) CreateOrUpdateRole(role *osv1.Role, namespace string) {
	existing, _ := c.authClient.Roles(namespace).Get(role.ObjectMeta.Name, metaV1.GetOptions{})

	if reflect.DeepEqual(osv1.Role{}, *existing) { // is empty
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
func (c ExternalOKD) CreateOrUpdateRoleBinding(binding *osv1.RoleBinding, namespace string) {
	bindingsSvc := c.authClient.RoleBindings(namespace)

	existing, _ := bindingsSvc.Get(binding.Name, metaV1.GetOptions{})

	if reflect.DeepEqual(osv1.RoleBinding{}, *existing) { // is empty
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

// Return the currently authenticated user name
func (c ExternalOKD) WhoAmI() string {
	user, e := c.userClient.Users().Get("~", metaV1.GetOptions{})
	if e != nil {
		panic(e)
	}

	return user.Name
}
