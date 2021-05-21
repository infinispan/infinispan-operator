package eventlog

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type ControllerWithEvents interface {
	EventRecorder() *record.EventRecorder
	Logger() *logr.Logger
	Client() *client.Client
}

type ControllerWithLogger interface {
	Name() string
	Logger() *logr.Logger
}

type ErrorEvent struct {
	E      error
	Reason string
	Owner  *runtime.Object
}

func (eev *ErrorEvent) Error() string {
	return eev.E.Error()
}

func ValuedLogger(cwl ControllerWithLogger, keysAndValues ...interface{}) logr.Logger {
	log := Logger(cwl)
	logger := log.WithValues(keysAndValues...)
	return logger
}

func Logger(cwl ControllerWithLogger) logr.Logger {
	if *cwl.Logger() != nil {
		return *cwl.Logger()
	}
	*cwl.Logger() = logf.Log.WithName(cwl.Name())
	return *cwl.Logger()
}

func LogAndSendEvent(cwe ControllerWithEvents, owner runtime.Object, reason, message string) {
	(*cwe.Logger()).Info(message)
	if *cwe.EventRecorder() != nil && owner != nil {
		(*cwe.EventRecorder()).Event(owner, corev1.EventTypeWarning, reason, message)
	}
}
