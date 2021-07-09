package utils

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v2alpha1"
	ispnv2 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v2alpha1"
	backupCtrl "github.com/infinispan/infinispan-operator/pkg/controller/backup"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnctrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	serviceCtrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/resources/service"
	restoreCtrl "github.com/infinispan/infinispan-operator/pkg/controller/restore"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/launcher"
	routev1 "github.com/openshift/api/route/v1"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Runtime scheme
var Scheme = runtime.NewScheme()

var log = logf.Log.WithName("kubernetes_test")

// TestKubernetes abstracts testing related interaction with a Kubernetes cluster
type TestKubernetes struct {
	Kubernetes *kube.Kubernetes
}

// MapperProvider is a function that provides RESTMapper instances
type MapperProvider func(cfg *rest.Config, opts ...apiutil.DynamicRESTMapperOption) (meta.RESTMapper, error)

func init() {
	addToScheme(&v1.SchemeBuilder, Scheme)
	addToScheme(&rbacv1.SchemeBuilder, Scheme)
	addToScheme(&apiextv1beta1.SchemeBuilder, Scheme)
	addToScheme(&ispnv1.SchemeBuilder.SchemeBuilder, Scheme)
	addToScheme(&ispnv2.SchemeBuilder.SchemeBuilder, Scheme)
	addToScheme(&appsv1.SchemeBuilder, Scheme)
	addToScheme(&storagev1.SchemeBuilder, Scheme)
	ExpectNoError(routev1.Install(Scheme))
}

func addToScheme(schemeBuilder *runtime.SchemeBuilder, scheme *runtime.Scheme) {
	err := schemeBuilder.AddToScheme(scheme)
	ExpectNoError(err)
}

// NewKubernetesFromLocalConfig creates a new Kubernetes instance from configuration.
// The configuration is resolved locally from known locations.
func NewKubernetesFromLocalConfig(scheme *runtime.Scheme, mapperProvider MapperProvider, ctx string) (*kube.Kubernetes, error) {
	config := resolveConfig(ctx)
	config = kube.SetConfigDefaults(config)
	mapper, err := mapperProvider(config)
	if err != nil {
		return nil, err
	}
	kubernetes, err := client.New(config, createOptions(scheme, mapper))
	if err != nil {
		return nil, err
	}
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}

	return &kube.Kubernetes{
		Client:     kubernetes,
		RestClient: restClient,
		RestConfig: config,
	}, nil
}

func createOptions(scheme *runtime.Scheme, mapper meta.RESTMapper) client.Options {
	return client.Options{
		Scheme: scheme,
		Mapper: mapper,
	}
}

func resolveConfig(ctx string) *rest.Config {
	internal, _ := rest.InClusterConfig()
	if internal == nil {
		kubeConfig := kube.FindKubeConfig()
		configOvr := clientcmd.ConfigOverrides{}
		if ctx != "" {
			configOvr.CurrentContext = ctx
		}
		clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfig},
			&configOvr)
		external, _ := clientConfig.ClientConfig()
		return external
	}
	return internal
}

// NewTestKubernetes creates a new instance of TestKubernetes
func NewTestKubernetes(ctx string) *TestKubernetes {
	mapperProvider := apiutil.NewDynamicRESTMapper
	kubernetes, err := NewKubernetesFromLocalConfig(Scheme, mapperProvider, ctx)
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

	err := k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: namespace}, obj)
	if err != nil && k8serrors.IsNotFound(err) {
		err = k.Kubernetes.Client.Create(context.TODO(), obj)
		ExpectNoError(err)
		return
	}
	ExpectNoError(err)
}

func (k TestKubernetes) CleanNamespaceAndLogOnPanic(namespace string, spacLabel map[string]string) {
	panicVal := recover()
	// Print pod output if a panic has occurred
	if panicVal != nil {
		k.PrintAllResources(namespace, &v1.PodList{}, map[string]string{"app": "infinispan-pod"})
		k.PrintAllResources(namespace, &v1.PodList{}, map[string]string{"app": "infinispan-batch-pod"})
		k.PrintAllResources(namespace, &appsv1.StatefulSetList{}, map[string]string{})
		k.PrintAllResources(namespace, &ispnv1.InfinispanList{}, map[string]string{})
		k.PrintAllResources(namespace, &ispnv2.BackupList{}, map[string]string{})
		k.PrintAllResources(namespace, &ispnv2.RestoreList{}, map[string]string{})
		k.PrintAllResources(namespace, &ispnv2.BatchList{}, map[string]string{})
		k.PrintAllResources(namespace, &ispnv2.CacheList{}, map[string]string{})
	}
	opts := []client.DeleteAllOfOption{
		client.InNamespace(namespace),
	}

	if CleanupInfinispan == "TRUE" || panicVal == nil {
		ctx := context.TODO()
<<<<<<< HEAD
		if spacLabel != nil {
			opts = []client.DeleteAllOfOption{
				client.InNamespace(namespace),
				client.MatchingLabels(spacLabel),
			}
=======
		if spacLabel == nil {
			ExpectMaybeNotFound(k.Kubernetes.Client.DeleteAllOf(ctx, &ispnv2.Batch{}, opts...))
			ExpectMaybeNotFound(k.Kubernetes.Client.DeleteAllOf(ctx, &ispnv2.Cache{}, opts...))
			ExpectMaybeNotFound(k.Kubernetes.Client.DeleteAllOf(ctx, &ispnv1.Infinispan{}, opts...))
		} else {
			opts := []client.DeleteAllOfOption{
				client.InNamespace(namespace),
				client.MatchingLabels(spacLabel),
			}

			ExpectMaybeNotFound(k.Kubernetes.Client.DeleteAllOf(ctx, &ispnv2.Batch{}, opts...))
			ExpectMaybeNotFound(k.Kubernetes.Client.DeleteAllOf(ctx, &ispnv2.Cache{}, opts...))
			ExpectMaybeNotFound(k.Kubernetes.Client.DeleteAllOf(ctx, &ispnv1.Infinispan{}, opts...))
>>>>>>> 0227575... issue-1070: Use testKube.CleanNamespaceAndLogOnPanic in all tests
		}

		ExpectMaybeNotFound(k.Kubernetes.Client.DeleteAllOf(ctx, &ispnv2.Batch{}, opts...))
		ExpectMaybeNotFound(k.Kubernetes.Client.DeleteAllOf(ctx, &ispnv2.Cache{}, opts...))
		ExpectMaybeNotFound(k.Kubernetes.Client.DeleteAllOf(ctx, &ispnv1.Infinispan{}, opts...))
		ExpectMaybeNotFound(k.Kubernetes.Client.DeleteAllOf(ctx, &ispnv2.Restore{}, opts...))
		ExpectMaybeNotFound(k.Kubernetes.Client.DeleteAllOf(ctx, &ispnv2.Backup{}, opts...))
		k.WaitForPods(0, 3*SinglePodTimeout, &client.ListOptions{Namespace: namespace, LabelSelector: labels.SelectorFromSet(map[string]string{"app": "infinispan-pod"})}, nil)
		k.WaitForPods(0, 3*SinglePodTimeout, &client.ListOptions{Namespace: namespace, LabelSelector: labels.SelectorFromSet(map[string]string{"app": "infinispan-batch-pod"})}, nil)
	}

	if panicVal != nil {
		// Throw the recovered panic values so the tests fail as expected
		panic(panicVal)
	}
}

func (k TestKubernetes) PrintAllResources(namespace string, list runtime.Object, set labels.Set) {
	if err := k.Kubernetes.ResourcesList(namespace, set, list); err != nil {
		LogError(err)
	}

	unstructuredResource, err := runtime.DefaultUnstructuredConverter.ToUnstructured(list)
	LogError(err)
	unstructuredResourceList := unstructured.UnstructuredList{}
	unstructuredResourceList.SetUnstructuredContent(unstructuredResource)

	for _, item := range unstructuredResourceList.Items {
		yaml, err := yaml.Marshal(item)
		LogError(err)
		if strings.Contains(reflect.TypeOf(list).String(), "PodList") {
			fmt.Println(strings.Repeat("-", 30))
			log, err := k.Kubernetes.Logs(item.GetName(), namespace)
			LogError(err)
			fmt.Printf("%s", log)
		}

		fmt.Println(strings.Repeat("-", 30))
		fmt.Println(string(yaml))
	}
}

// DeleteNamespace deletes a namespace
func (k TestKubernetes) DeleteNamespace(namespace string) {
	fmt.Printf("Delete namespace %s\n", namespace)
	obj := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	err := k.Kubernetes.Client.Delete(context.TODO(), obj, DeleteOpts...)
	ExpectMaybeNotFound(err)

	fmt.Println("Waiting for the namespace to be removed")
	err = wait.Poll(DefaultPollPeriod, MaxWaitTimeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: namespace}, obj)
		if err != nil && k8serrors.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	})
	ExpectNoError(err)
}

func (k TestKubernetes) GetBackup(name, namespace string) *ispnv2.Backup {
	backup := &ispnv2.Backup{}
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectMaybeNotFound(k.Kubernetes.Client.Get(context.TODO(), key, backup))
	return backup
}

func (k TestKubernetes) GetRestore(name, namespace string) *ispnv2.Restore {
	restore := &ispnv2.Restore{}
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectMaybeNotFound(k.Kubernetes.Client.Get(context.TODO(), key, restore))
	return restore
}

func (k TestKubernetes) GetBatch(name, namespace string) *ispnv2.Batch {
	batch := &ispnv2.Batch{}
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectMaybeNotFound(k.Kubernetes.Client.Get(context.TODO(), key, batch))
	return batch
}

func (k TestKubernetes) Create(obj runtime.Object) {
	ExpectNoError(k.Kubernetes.Client.Create(context.TODO(), obj))
}

// CreateInfinispan creates an Infinispan resource in the given namespace
func (k TestKubernetes) CreateInfinispan(infinispan *ispnv1.Infinispan, namespace string) {
	infinispan.Namespace = namespace
	err := k.Kubernetes.Client.Create(context.TODO(), infinispan)
	ExpectNoError(err)
}

func (k TestKubernetes) DeleteInfinispan(infinispan *ispnv1.Infinispan, timeout time.Duration) {
	labelSelector := labels.SelectorFromSet(ispnctrl.PodLabels(infinispan.Name))
	k.DeleteResource(infinispan.Namespace, labelSelector, infinispan, timeout)
}

func (k TestKubernetes) DeleteBackup(backup *ispnv2.Backup) {
	labelSelector := labels.SelectorFromSet(backupCtrl.PodLabels(backup.Name, backup.Spec.Cluster))
	k.DeleteResource(backup.Namespace, labelSelector, backup, SinglePodTimeout)
}

func (k TestKubernetes) DeleteRestore(restore *ispnv2.Restore) {
	labelSelector := labels.SelectorFromSet(restoreCtrl.PodLabels(restore.Name, restore.Spec.Cluster))
	k.DeleteResource(restore.Namespace, labelSelector, restore, SinglePodTimeout)
}

func (k TestKubernetes) DeleteBatch(batch *ispnv2.Batch) {
	labelSelector := labels.SelectorFromSet(restoreCtrl.PodLabels(batch.Name, batch.Spec.Cluster))
	k.DeleteResource(batch.Namespace, labelSelector, batch, SinglePodTimeout)
}

// DeleteResource deletes the k8 resource and waits that all the pods and pvs associated with that resource are gone
func (k TestKubernetes) DeleteResource(namespace string, selector labels.Selector, obj runtime.Object, timeout time.Duration) {
	err := k.Kubernetes.Client.Delete(context.TODO(), obj, DeleteOpts...)
	ExpectMaybeNotFound(err)

	listOps := &client.ListOptions{Namespace: namespace, LabelSelector: selector}
	podList := &v1.PodList{}
	err = wait.Poll(DefaultPollPeriod, timeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.List(context.TODO(), podList, listOps)
		if err != nil || len(podList.Items) != 0 {
			return false, nil
		}
		return true, nil
	})
	ExpectNoError(err)
	// Check that PersistentVolumeClaims have been cleanup
	err = wait.Poll(DefaultPollPeriod, timeout, func() (done bool, err error) {
		pvc := &v1.PersistentVolumeClaimList{}
		err = k.Kubernetes.Client.List(context.TODO(), pvc, listOps)
		if err != nil || len(pvc.Items) != 0 {
			return false, nil
		}
		return true, nil
	})
	ExpectNoError(err)
}

// GracefulShutdownInfinispan deletes the infinispan resource
// and waits that all the pods are gone
func (k TestKubernetes) GracefulShutdownInfinispan(infinispan *ispnv1.Infinispan) {
	err := k.UpdateInfinispan(infinispan, func() {
		infinispan.Spec.Replicas = 0
	})
	ExpectNoError(err)
	k.WaitForInfinispanCondition(infinispan.Name, infinispan.Namespace, ispnv1.ConditionGracefulShutdown)
}

// GracefulRestartInfinispan restarts the infinispan resource and waits that cluster is WellFormed
func (k TestKubernetes) GracefulRestartInfinispan(infinispan *ispnv1.Infinispan, replicas int32, timeout time.Duration) {
	err := wait.Poll(DefaultPollPeriod, timeout, func() (done bool, err error) {
		updErr := k.UpdateInfinispan(infinispan, func() {
			infinispan.Spec.Replicas = replicas
		})

		if updErr != nil {
			fmt.Printf("Error updating infinispan %v\n", updErr)
			return false, nil
		}
		return true, nil
	})
	ExpectNoError(err)

	k.WaitForInfinispanCondition(infinispan.Name, infinispan.Namespace, ispnv1.ConditionWellFormed)
}

func (k TestKubernetes) UpdateInfinispan(ispn *ispnv1.Infinispan, update func()) error {
	err := wait.Poll(DefaultPollPeriod, MaxWaitTimeout, func() (done bool, err error) {
		_, updateErr := controllerutil.CreateOrUpdate(context.TODO(), k.Kubernetes.Client, ispn, func() error {
			if ispn.CreationTimestamp.IsZero() {
				return k8serrors.NewNotFound(ispnv1.Resource("infinispan.org"), ispn.Name)
			}
			// Change the Infinispan spec
			if update != nil {
				update()
			}
			// Workaround for OpenShift local test (clear GVK on decode in the client)
			ispn.TypeMeta = InfinispanTypeMeta

			return nil
		})
		if updateErr == nil || k8serrors.IsConflict(updateErr) {
			return true, nil
		}
		return false, updateErr
	})

	return err
}

// CreateOrUpdateAndWaitForCRD creates or updates a Custom Resource Definition (CRD), waiting it to become ready.
func (k TestKubernetes) CreateOrUpdateAndWaitForCRD(crd *apiextv1beta1.CustomResourceDefinition) {
	fmt.Printf("Create or update CRD %s\n", crd.Name)

	customResourceObject := &apiextv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: crd.Name,
		},
	}
	result, err := controllerutil.CreateOrUpdate(context.TODO(), k.Kubernetes.Client, customResourceObject, func() error {
		customResourceObject.Spec = crd.Spec
		return nil
	})
	ExpectNoError(err)

	fmt.Println("Wait for CRD to be established")
	err = wait.Poll(DefaultPollPeriod, MaxWaitTimeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: crd.Name}, crd)
		if err != nil {
			return false, fmt.Errorf("unable to get CRD: %w", err)
		}

		for _, cond := range crd.Status.Conditions {
			switch cond.Type {
			case apiextv1beta1.Established:
				if cond.Status == apiextv1beta1.ConditionTrue {
					fmt.Printf("CRD %s\n", result)
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
func (k TestKubernetes) WaitForExternalService(ispn *ispnv1.Infinispan, timeout time.Duration, client HTTPClient) string {
	var hostAndPort string
	err := wait.Poll(DefaultPollPeriod, timeout, func() (done bool, err error) {
		switch ispn.GetExposeType() {
		case ispnv1.ExposeTypeNodePort, ispnv1.ExposeTypeLoadBalancer:
			routeList := &v1.ServiceList{}
			err = k.Kubernetes.ResourcesList(ispn.Namespace, serviceCtrl.ExternalServiceLabels(ispn.Name), routeList)
			ExpectNoError(err)

			if len(routeList.Items) > 0 {
				switch ispn.GetExposeType() {
				case ispnv1.ExposeTypeNodePort:
					host, err := k.Kubernetes.GetNodeHost(log)
					ExpectNoError(err)
					hostAndPort = fmt.Sprintf("%s:%d", host, getNodePort(&routeList.Items[0]))
				case ispnv1.ExposeTypeLoadBalancer:
					hostAndPort = k.Kubernetes.GetExternalAddress(&routeList.Items[0])
				}
			}
		case ispnv1.ExposeTypeRoute:
			routeList := &routev1.RouteList{}
			err = k.Kubernetes.ResourcesList(ispn.Namespace, serviceCtrl.ExternalServiceLabels(ispn.Name), routeList)
			ExpectNoError(err)
			if len(routeList.Items) > 0 {
				hostAndPort = routeList.Items[0].Spec.Host
			}
		}
		if hostAndPort == "" {
			return false, nil
		}
		return CheckExternalAddress(client, hostAndPort), nil
	})
	ExpectNoError(err)
	return hostAndPort
}

func getNodePort(service *corev1.Service) int32 {
	return service.Spec.Ports[0].NodePort
}

func CheckExternalAddress(c HTTPClient, hostAndPort string) bool {
	httpURL := fmt.Sprintf("%s/%s", hostAndPort, consts.ServerHTTPHealthPath)
	resp, err := c.Get(httpURL, nil)
	if isTemporary(err) {
		return false
	}
	ExpectNoError(err)
	defer LogError(resp.Body.Close())
	log.Info("Received response for external address", "response code", resp.StatusCode)
	return resp.StatusCode == http.StatusOK
}

func isTemporary(err error) bool {
	if errors.Is(err, io.EOF) {
		// Connection closures may be resolved upon retry, and are thus treated as temporary.
		return true
	}

	var temporary interface {
		Temporary() bool
	}

	var timeout interface {
		Timeout() bool
	}
	return errors.As(err, &temporary) || errors.As(err, &timeout)
}

func (k TestKubernetes) WaitForInfinispanPods(required int, timeout time.Duration, cluster, namespace string) {
	k.WaitForPods(required, timeout, &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: labels.SelectorFromSet(ispnctrl.PodLabels(cluster)),
	}, nil)
}

// WaitForPods waits for pods with given ListOptions to reach the desired count in ContainersReady state
func (k TestKubernetes) WaitForPods(required int, timeout time.Duration, listOps *client.ListOptions, callback func([]v1.Pod) bool) {
	podList := &v1.PodList{}
	err := wait.Poll(DefaultPollPeriod, timeout, func() (done bool, err error) {
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

		if allReady && callback != nil {
			return callback(pods), nil
		}
		return allReady, nil
	})

	ExpectNoError(err)
}

func (k TestKubernetes) WaitForInfinispanCondition(name, namespace string, condition ispnv1.ConditionType) *ispnv1.Infinispan {
	ispn := &ispnv1.Infinispan{}
	err := wait.Poll(ConditionPollPeriod, ConditionWaitTimeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, ispn)
		if err != nil && k8serrors.IsNotFound(err) {
			return false, err
		}
		if err != nil {
			return false, nil
		}
		if ispn.IsConditionTrue(condition) {
			log.Info("infinispan condition met", "condition", condition, "status", metav1.ConditionTrue)
			return true, nil
		}
		return false, nil
	})
	ExpectNoError(err)
	return ispn
}

func (k TestKubernetes) GetSchemaForRest(ispn *ispnv1.Infinispan) string {
	curr := ispnv1.Infinispan{}
	// Wait for the operator to populate Infinispan CR data
	err := wait.Poll(DefaultPollPeriod, SinglePodTimeout, func() (done bool, err error) {
		if err := k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, &curr); err != nil {
			return false, nil
		}
		return len(curr.Status.Conditions) > 0, nil
	})
	ExpectNoError(err)
	return curr.GetEndpointScheme()
}

func debugPods(required int, pods []v1.Pod) {
	log.Info("pod list incomplete", "required", required, "pod list size", len(pods))
	for _, pod := range pods {
		log.Info("pod info", "name", pod.Name, "statuses", pod.Status.ContainerStatuses)
	}
}

// DeleteCRD deletes a CustomResourceDefinition
func (k TestKubernetes) DeleteCRD(name string) {
	crd := &apiextv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	err := k.Kubernetes.Client.Delete(context.TODO(), crd, DeleteOpts...)
	ExpectMaybeNotFound(err)

	fmt.Println("Waiting for the CRD to be removed")
	err = wait.Poll(DefaultPollPeriod, MaxWaitTimeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: name}, crd)
		if err != nil && k8serrors.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	})
	ExpectNoError(err)
}

// Nodes returns a list of running nodes in the cluster
func (k TestKubernetes) Nodes() []string {
	nodes := &v1.NodeList{}
	listOps := &client.ListOptions{}
	err := k.Kubernetes.Client.List(context.TODO(), nodes, listOps)
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

// GetSecret gets a secret
func (k TestKubernetes) GetSecret(name, namespace string) *corev1.Secret {
	secret := &corev1.Secret{}
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	ExpectMaybeNotFound(k.Kubernetes.Client.Get(context.TODO(), key, secret))
	return secret
}

// UpdateSecret updates a secret
func (k TestKubernetes) UpdateSecret(secret *v1.Secret) {
	err := k.Kubernetes.Client.Update(context.TODO(), secret)
	ExpectNoError(err)
}

// CreateSecret creates a secret
func (k TestKubernetes) CreateSecret(secret *v1.Secret) {
	err := k.Kubernetes.Client.Create(context.TODO(), secret)
	ExpectNoError(err)
}

// DeleteSecret deletes a secret
func (k TestKubernetes) DeleteSecret(secret *v1.Secret) {
	err := k.Kubernetes.Client.Delete(context.TODO(), secret, DeleteOpts...)
	ExpectMaybeNotFound(err)
}

// RunOperator runs an operator on a Kubernetes cluster
func (k TestKubernetes) RunOperator(namespace, crdsPath string) chan struct{} {
	k.installCRD(crdsPath + "infinispan.org_infinispans_crd.yaml")
	k.installCRD(crdsPath + "infinispan.org_caches_crd.yaml")
	k.installCRD(crdsPath + "infinispan.org_backups_crd.yaml")
	k.installCRD(crdsPath + "infinispan.org_restores_crd.yaml")
	k.installCRD(crdsPath + "infinispan.org_batches_crd.yaml")
	stopCh := make(chan struct{})
	go runOperatorLocally(stopCh, namespace)
	return stopCh
}

func (k TestKubernetes) installCRD(path string) {
	crd := &apiextv1beta1.CustomResourceDefinition{}
	k.LoadResourceFromYaml(path, crd)
	k.CreateOrUpdateAndWaitForCRD(crd)
}

func (k TestKubernetes) LoadResourceFromYaml(path string, obj runtime.Object) {
	yamlReader, err := GetYamlReaderFromFile(path)
	ExpectNoError(err)
	y, err := yamlReader.Read()
	ExpectNoError(err)
	ExpectNoError(k8syaml.NewYAMLToJSONDecoder(strings.NewReader(string(y))).Decode(&obj))
}

func RunOperator(m *testing.M, k *TestKubernetes) {
	namespace := strings.ToLower(Namespace)
	if "TRUE" == RunLocalOperator {
		if "TRUE" != RunSaOperator && OperatorUpgradeStage != OperatorUpgradeStageTo {
			k.DeleteNamespace(namespace)
			k.DeleteCRD("infinispans.infinispan.org")
			k.DeleteCRD("caches.infinispan.org")
			k.DeleteCRD("backup.infinispan.org")
			k.DeleteCRD("restore.infinispan.org")
			k.DeleteCRD("batch.infinispan.org")
			k.NewNamespace(namespace)
		}
		stopCh := k.RunOperator(namespace, "../../../deploy/crds/")
		code := m.Run()
		close(stopCh)
		os.Exit(code)
	} else {
		code := m.Run()
		os.Exit(code)
	}
}

// Run the operator locally
func runOperatorLocally(stopCh chan struct{}, namespace string) {
	_ = os.Setenv("WATCH_NAMESPACE", namespace)
	_ = os.Setenv("KUBECONFIG", kube.FindKubeConfig())
	_ = os.Setenv("OSDK_FORCE_RUN_MODE", "local")
	_ = os.Setenv("OPERATOR_NAME", OperatorName)
	launcher.Launch(launcher.Parameters{StopChannel: stopCh})
}
func (k TestKubernetes) DeleteCache(cache *ispnv2.Cache) {
	err := k.Kubernetes.Client.Delete(context.TODO(), cache)
	ExpectMaybeNotFound(err)
}

func (k TestKubernetes) WaitForCacheCondition(name, namespace string, condition v2alpha1.CacheCondition) {
	cache := &v2alpha1.Cache{}
	err := wait.Poll(ConditionPollPeriod, ConditionWaitTimeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, cache)
		if err != nil && k8serrors.IsNotFound(err) {
			return false, err
		}
		if err != nil {
			return false, nil
		}
		for _, c := range cache.Status.Conditions {
			if strings.EqualFold(c.Type, condition.Type) && (c.Status == condition.Status) {
				log.Info("Cache condition met", "condition", condition)
				return true, nil
			}
		}
		return false, nil
	})
	ExpectNoError(err)
}

func GetServerName(i *ispnv1.Infinispan) string {
	if i.GetExposeType() != ispnv1.ExposeTypeRoute {
		return "server"
	}
	c := clientcmd.GetConfigFromFileOrDie(kube.FindKubeConfig())
	clusterName := c.Contexts[c.CurrentContext].Cluster
	cluster := c.Clusters[clusterName]
	url, err := url.Parse(cluster.Server)
	ExpectNoError(err)
	hostname := strings.Split(strings.Replace(url.Host, "api.", "apps.", 1), ":")[0]
	return fmt.Sprintf("%s-%s.%s", i.GetServiceExternalName(), i.GetNamespace(), hostname)
}
