package base

import (
	"github.com/go-logr/logr"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type ReconcilerBase struct {
	Name string
	client.Client
	*runtime.Scheme
	*kubernetes.Kubernetes
	record.EventRecorder
	log *logr.Logger
}

func NewReconcilerBase(name string, client client.Client, scheme *runtime.Scheme, kubernetes *kubernetes.Kubernetes, recorder record.EventRecorder) *ReconcilerBase {
	return &ReconcilerBase{Name: name, Client: client, Scheme: scheme, Kubernetes: kubernetes, EventRecorder: recorder}
}

func NewReconcilerBaseFromManager(name string, mgr manager.Manager) *ReconcilerBase {
	return &ReconcilerBase{Name: name, Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Kubernetes: kubernetes.NewKubernetesFromController(mgr), EventRecorder: mgr.GetEventRecorderFor(name)}
}

func (rb *ReconcilerBase) InitLogger(keysAndValues ...interface{}) {
	logger := logf.Log.WithName(rb.Name).WithValues(keysAndValues...)
	rb.log = &logger
}

func (rb *ReconcilerBase) Logger() logr.Logger {
	if rb.log != nil {
		return *rb.log
	}
	return logf.Log.WithName(rb.Name)
}

func (rb *ReconcilerBase) LogAndSendEvent(owner runtime.Object, message, reason string) {
	rb.Logger().Info(message)
	if rb.EventRecorder != nil && owner != nil {
		rb.Event(owner, corev1.EventTypeWarning, reason, message)
	}
}
