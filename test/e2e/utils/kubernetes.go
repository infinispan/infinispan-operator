package utils

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v2alpha1"
	ispnv2 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v2alpha1"
	backupCtrl "github.com/infinispan/infinispan-operator/pkg/controller/backup"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnctrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	restoreCtrl "github.com/infinispan/infinispan-operator/pkg/controller/restore"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/launcher"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
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
	addToScheme(&ispnv2.SchemeBuilder.SchemeBuilder, scheme)
	addToScheme(&appsv1.SchemeBuilder, scheme)
	addToScheme(&storagev1.SchemeBuilder, scheme)
	ExpectNoError(routev1.Install(scheme))
}

func addToScheme(schemeBuilder *runtime.SchemeBuilder, scheme *runtime.Scheme) {
	err := schemeBuilder.AddToScheme(scheme)
	ExpectNoError(err)
}

// NewTestKubernetes creates a new instance of TestKubernetes
func NewTestKubernetes(ctx string) *TestKubernetes {
	mapperProvider := apiutil.NewDynamicRESTMapper
	kubernetes, err := kube.NewKubernetesFromLocalConfig(scheme, mapperProvider, ctx)
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
			Name: namespace,
		},
	}
	err := k.Kubernetes.Client.Delete(context.TODO(), obj, DeleteOpts...)
	ExpectMaybeNotFound(err)

	fmt.Println("Waiting for the namespace to be removed")
	err = wait.Poll(DefaultPollPeriod, MaxWaitTimeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: namespace}, obj)
		if err != nil && errors.IsNotFound(err) {
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
	ns := types.NamespacedName{Namespace: infinispan.Namespace, Name: infinispan.Name}
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

	err = wait.Poll(DefaultPollPeriod, timeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), ns, infinispan)
		if err != nil || !infinispan.IsWellFormed() {
			return false, nil
		}
		return true, nil
	})
	ExpectNoError(err)
}

func (k TestKubernetes) UpdateInfinispan(ispn *ispnv1.Infinispan, update func()) error {
	err := wait.Poll(DefaultPollPeriod, MaxWaitTimeout, func() (done bool, err error) {
		_, updateErr := controllerutil.CreateOrUpdate(context.TODO(), k.Kubernetes.Client, ispn, func() error {
			if ispn.CreationTimestamp.IsZero() {
				return errors.NewNotFound(ispnv1.Resource("infinispan.org"), ispn.Name)
			}
			// Change the Infinispan spec
			if update != nil {
				update()
			}
			// Workaround for OpenShift local test (clear GVK on decode in the client)
			ispn.TypeMeta = InfinispanTypeMeta

			return nil
		})
		if updateErr == nil || errors.IsConflict(updateErr) {
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
			return false, fmt.Errorf("unable to get CRD: %v", err)
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
func (k TestKubernetes) WaitForExternalService(routeName, namespace string, exposeType ispnv1.ExposeType, timeout time.Duration, client HTTPClient) string {
	var hostAndPort string
	err := wait.Poll(DefaultPollPeriod, timeout, func() (done bool, err error) {
		switch exposeType {
		case ispnv1.ExposeTypeNodePort, ispnv1.ExposeTypeLoadBalancer:
			route := &v1.Service{}
			err = k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: routeName}, route)
			ExpectNoError(err)

			// depending on the k8s cluster setting, service is sometime available
			// via Ingress address sometime via node port. So trying both the methods
			// Try node port first
			host, err := k.Kubernetes.GetNodeHost(log)
			ExpectNoError(err)
			hostAndPort = fmt.Sprintf("%s:%d", host, k.Kubernetes.GetNodePort(route))
			result := checkExternalAddress(client, hostAndPort)
			if result {
				return result, nil
			}

			// if node port fails and it points to 127.0.0.1,
			// it could be running kubernetes-inside-docker,
			// so try using cluster ip instead
			if strings.Contains(hostAndPort, "127.0.0.1") {
				log.Info("Running on kind (kubernetes-inside-docker), try accessing via node port forward")
				hostAndPort = fmt.Sprintf("127.0.0.1:%d", consts.InfinispanUserPort)
				result := checkExternalAddress(client, hostAndPort)
				if result {
					return result, nil
				}
			}

			// then try to get ingress information
			hostAndPort = k.Kubernetes.GetExternalAddress(route)
			if hostAndPort == "" {
				return false, nil
			}
		case ispnv1.ExposeTypeRoute:
			route := &routev1.Route{}
			err = k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: routeName}, route)
			ExpectNoError(err)
			if route.Spec.Host != "" {
				hostAndPort = route.Spec.Host
			}
		}
		return checkExternalAddress(client, hostAndPort), nil
	})
	ExpectNoError(err)
	return hostAndPort
}

func checkExternalAddress(c HTTPClient, hostAndPort string) bool {
	httpURL := fmt.Sprintf("%s/%s", hostAndPort, consts.ServerHTTPHealthPath)
	resp, err := c.Get(httpURL, nil)
	ExpectNoError(err)
	log.Info("Received response for external address", "response code", resp.StatusCode)
	return resp.StatusCode == http.StatusOK
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

func (k TestKubernetes) WaitForInfinispanCondition(name, namespace string, condition ispnv1.ConditionType) {
	ispn := &ispnv1.Infinispan{}
	err := wait.Poll(ConditionPollPeriod, ConditionWaitTimeout, func() (done bool, err error) {
		err = k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, ispn)
		if err != nil && errors.IsNotFound(err) {
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
		if err != nil && errors.IsNotFound(err) {
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

// CreateSecret creates a secret
func (k TestKubernetes) CreateSecret(secret *v1.Secret) {
	err := k.Kubernetes.Client.Create(context.TODO(), secret)
	ExpectNoError(err)
}

// DeleteSecret deletes a secret
func (k TestKubernetes) DeleteSecret(secret *v1.Secret) {
	err := k.Kubernetes.Client.Delete(context.TODO(), secret, DeleteOpts...)
	ExpectNoError(err)
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
	yamlReader, err := GetYamlReaderFromFile(path)
	ExpectNoError(err)
	y, err := yamlReader.Read()
	ExpectNoError(err)
	crd := apiextv1beta1.CustomResourceDefinition{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(y))).Decode(&crd)
	ExpectNoError(err)
	k.CreateOrUpdateAndWaitForCRD(&crd)
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
	kubeConfig := kube.FindKubeConfig()
	_ = os.Setenv("WATCH_NAMESPACE", namespace)
	_ = os.Setenv("KUBECONFIG", kubeConfig)
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
		if err != nil && errors.IsNotFound(err) {
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
