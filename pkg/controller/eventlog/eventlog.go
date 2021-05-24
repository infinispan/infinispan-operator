package eventlog

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type ReasonType string

type EventLogger interface {
	Name() string
	EventRecorder() *record.EventRecorder
	Logger() *logr.Logger
}

type ErrorEvent struct {
	E      error
	Reason ReasonType
	Owner  *runtime.Object
}

func (eev *ErrorEvent) Error() string {
	return eev.E.Error()
}

func ValuedLogger(cwe EventLogger, keysAndValues ...interface{}) logr.Logger {
	log := Logger(cwe)
	logger := log.WithValues(keysAndValues...)
	return logger
}

func Logger(el EventLogger) logr.Logger {
	if *el.Logger() != nil {
		return *el.Logger()
	}
	*el.Logger() = logf.Log.WithName(el.Name())
	return *el.Logger()
}

func LogAndSendEvent(el EventLogger, owner runtime.Object, reason ReasonType, message string) {
	(*el.Logger()).Info(message)
	if *el.EventRecorder() != nil && owner != nil {
		(*el.EventRecorder()).Event(owner, corev1.EventTypeWarning, string(reason), message)
	}
}
