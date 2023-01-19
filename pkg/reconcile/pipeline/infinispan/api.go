package infinispan

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	ispnApi "github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	config "github.com/infinispan/infinispan-operator/pkg/infinispan/configuration/server"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	ingressv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Pipeline for Infinispan reconciliation
type Pipeline interface {
	// Process the pipeline
	// Returns true if processing should be repeated and optional error if occurred
	// important: even if error occurred it might not be needed to retry processing
	Process(ctx context.Context) (bool, time.Duration, error)
}

// Handler an individual stage in the pipeline
type Handler interface {
	Handle(i *ispnv1.Infinispan, ctx Context)
}

type HandlerFunc func(i *ispnv1.Infinispan, ctx Context)

func (f HandlerFunc) Handle(i *ispnv1.Infinispan, ctx Context) {
	f(i, ctx)
}

// FlowStatus Pipeline flow control
type FlowStatus struct {
	Retry bool
	Stop  bool
	Err   error
	Delay time.Duration
}

func (f *FlowStatus) String() string {
	return fmt.Sprintf("Requeue=%t, Stop=%t, Err=%s, Delay=%dms", f.Retry, f.Stop, f.Err.Error(), f.Delay.Milliseconds())
}

// ContextProvider interface used by Pipeline implementations to obtain a Context
type ContextProvider interface {
	Get(ctx context.Context, config *ContextProviderConfig) (Context, error)
}

type ContextProviderConfig struct {
	DefaultAnnotations map[string]string
	DefaultLabels      map[string]string
	Infinispan         *ispnv1.Infinispan
	Logger             logr.Logger
	SupportedTypes     map[schema.GroupVersionKind]struct{} // We only care about keys, so use struct{} as it requires 0 bytes
	VersionManager     *version.Manager
}

// Context of the pipeline, which is passed to each Handler
type Context interface {

	// Operand returns the metadata associated with the current Infinispan CR spec.version field
	Operand() version.Operand

	// Operands returns the version.Manager containing all supported Operands
	Operands() *version.Manager

	// InfinispanClient returns a client for the Operand servers
	// The client is created Lazily and cached per Pipeline execution to prevent repeated calls to retrieve the cluster pods
	// An error is thrown on initial client creation if the cluster pods can't be retrieved or don't exist.
	InfinispanClient() (ispnApi.Infinispan, error)

	// InfinispanClientForPod returns a client for the specific pod
	InfinispanClientForPod(podName string) ispnApi.Infinispan

	// InfinispanPods returns all pods associated with the Infinispan cluster's StatefulSet
	// The list is created Lazily and cached per Pipeline execution to prevent repeated calls to retrieve the cluster pods
	// If an error is returned, then RetryProcessing is automatically set
	InfinispanPods() (*corev1.PodList, error)

	// ConfigFiles returns the ConfigFiles struct used to hold all configuration data required by the Operand
	ConfigFiles() *ConfigFiles

	// Resources interface provides convenience functions for interacting with kubernetes resources
	Resources() Resources

	// Ctx the Pipeline's context.Context that should be passed to any functions requiring a context
	Ctx() context.Context

	// Log the Infinispan request logger
	Log() logr.Logger

	// EventRecorder associated with the Infinispan controller
	EventRecorder() record.EventRecorder

	// Kubernetes exposes the underlying kubernetes client for when Resources doesn't provide the required functionality.
	// In general this method should be avoided if the same behaviour can be performed via the Resources interface
	Kubernetes() *kubernetes.Kubernetes

	// DefaultAnnotations defined for the Operator via ENV vars
	DefaultAnnotations() map[string]string

	// DefaultLabels defined for the Operator via ENV vars
	DefaultLabels() map[string]string

	// IsTypeSupported returns true if the GVK is supported on the kubernetes cluster
	IsTypeSupported(gvk schema.GroupVersionKind) bool

	// UpdateInfinispan updates the Infinispan CR resource being reconciled
	// If an error is encountered, then RetryProcessing is automatically set
	UpdateInfinispan(func()) error

	// Requeue indicates that the pipeline should stop once the current Handler has finished execution and
	// reconciliation should be requeued
	Requeue(reason error)

	// RequeueAfter indicates that the pipeline should stop once the current Handler has finished execution and
	// reconciliation should be requeued after delay time
	RequeueAfter(delay time.Duration, reason error)

	// RequeueEventually indicates that pipeline execution should continue after the current handler, but the
	// reconciliation event should be requeued on pipeline completion or when a call to Requeue is made by a
	// subsequent Handler execution. A non-zero delay indicates that on pipeline completion, the request should be
	// requeued after delay time.
	RequeueEventually(delay time.Duration)

	// Stop indicates that the pipeline should stop once the current Handler has finished execution
	Stop(err error)

	// FlowStatus the current status of the Pipeline
	FlowStatus() FlowStatus
}

// OperationResult is the result of a CreateOrPatch or CreateOrUpdate call
type OperationResult string

const ( // They should complete the sentence "Deployment default/foo has been ..."
	// OperationResultNone means that the resource has not been changed
	OperationResultNone OperationResult = "unchanged"
	// OperationResultCreated means that a new resource is created
	OperationResultCreated OperationResult = "created"
	// OperationResultUpdated means that an existing resource is updated
	OperationResultUpdated OperationResult = "updated"
	// OperationResultUpdatedStatus means that an existing resource and its status is updated
	OperationResultUpdatedStatus OperationResult = "updatedStatus"
	// OperationResultUpdatedStatusOnly means that only an existing status is updated
	OperationResultUpdatedStatusOnly OperationResult = "updatedStatusOnly"
)

// Resources interface that provides common functionality for interacting with Kubernetes resources
type Resources interface {
	// Create the passed object in the Infinispan namespace, setting the objects ControllerRef to the Infinispan CR if
	// setControllerRef is true
	Create(obj client.Object, setControllerRef bool, opts ...func(config *ResourcesConfig)) error
	// CreateOrUpdate the passed object in the Infinispan namespace, setting the objects ControllerRef to the Infinispan CR if
	// setControllerRef is true
	CreateOrUpdate(obj client.Object, setControllerRef bool, mutate func() error, opts ...func(config *ResourcesConfig)) (OperationResult, error)
	// CreateOrPatch the passed object in the Infinispan namespace, setting the objects ControllerRef to the Infinispan CR if
	// setControllerRef is true
	CreateOrPatch(obj client.Object, setControllerRef bool, mutate func() error, opts ...func(config *ResourcesConfig)) (OperationResult, error)
	// Delete the obj from the Infinispan namespace
	// IgnoreFoundError option is always true
	Delete(name string, obj client.Object, opts ...func(config *ResourcesConfig)) error
	// List resources in the Infinispan namespace using the passed set as a LabelSelector
	List(set map[string]string, list client.ObjectList, opts ...func(config *ResourcesConfig)) error
	// Load a resource from the Infinispan namespace
	Load(name string, obj client.Object, opts ...func(config *ResourcesConfig)) error
	// LoadGlobal loads a cluster scoped kubernetes resource
	LoadGlobal(name string, obj client.Object, opts ...func(config *ResourcesConfig)) error
	// SetControllerReference Set the controller reference of the passed object to the Infinispan CR being reconciled
	SetControllerReference(controlled metav1.Object) error
	// Update a kubernetes resource in the Infinispan namespace
	Update(obj client.Object, opts ...func(config *ResourcesConfig)) error
}

// ResourcesConfig config used by Resources implementations to control implementation behaviour
type ResourcesConfig struct {
	IgnoreNotFound  bool
	InvalidateCache bool
	RetryOnErr      bool
	SkipEventRec    bool
}

// IgnoreNotFound return nil when NotFound errors are present
func IgnoreNotFound(config *ResourcesConfig) {
	config.IgnoreNotFound = true
}

// RetryOnErr set Context#Requeue(err) when an error is encountered
func RetryOnErr(config *ResourcesConfig) {
	config.RetryOnErr = true
}

// InvalidateCache ignore any cached resources and execute a new call to the api-server
// Only applicable for Load and LoadGlobal functions
func InvalidateCache(config *ResourcesConfig) {
	config.InvalidateCache = true
}

// SkipEventRec do not send an event to the EventRecorder in the event of an error
// Only applicable for Load and LoadGlobal functions
func SkipEventRec(config *ResourcesConfig) {
	config.SkipEventRec = true
}

// ConfigFiles is used to hold all configuration required by the Operand in provisioned resources
type ConfigFiles struct {
	ConfigSpec             config.Spec
	ServerAdminConfig      string
	ServerBaseConfig       string
	ZeroConfig             string
	Log4j                  string
	UserIdentities         []byte
	AdminIdentities        *AdminIdentities
	CredentialStoreEntries map[string][]byte
	IdentitiesBatch        string
	UserConfig             UserConfig
	Keystore               *Keystore
	Truststore             *Truststore
	Transport              Transport
	XSite                  *XSite
}

type UserConfig struct {
	ServerConfig         string
	ServerConfigFileName string
	Log4j                string
}

type AdminIdentities struct {
	Username       string
	Password       string
	IdentitiesFile []byte
	CliProperties  string
}

type Keystore struct {
	Alias    string
	File     []byte
	PemFile  []byte
	Password string
	Path     string
	Type     string
}

type Truststore struct {
	File     []byte
	Password string
	Path     string
	Type     string
}

type Transport struct {
	Keystore   *Keystore
	Truststore *Truststore
}

type XSite struct {
	GossipRouter  GossipRouter
	MaxRelayNodes int32
	Sites         []BackupSite
}

type GossipRouter struct {
	Keystore   *Keystore
	Truststore *Truststore
}

type BackupSite struct {
	Address            string
	Name               string
	Port               int32
	IgnoreGossipRouter bool
}

var (
	ServiceTypes      = []schema.GroupVersionKind{ServiceGVK, RouteGVK, IngressGVK}
	ServiceGVK        = corev1.SchemeGroupVersion.WithKind("Service")
	RouteGVK          = routev1.SchemeGroupVersion.WithKind("Route")
	IngressGVK        = ingressv1.SchemeGroupVersion.WithKind("Ingress")
	ServiceMonitorGVK = monitoringv1.SchemeGroupVersion.WithKind("ServiceMonitor")
)
