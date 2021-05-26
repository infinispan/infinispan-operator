package eventlog

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type EventLogger interface {
	EventRecorder() *record.EventRecorder
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

func ValuedLogger(el EventLogger, name string, keysAndValues ...interface{}) logr.Logger {
	log := Logger(el, name)
	logger := log.WithValues(keysAndValues...)
	return logger
}

func Logger(el EventLogger, name string) logr.Logger {
	if *el.Logger() != nil {
		return *el.Logger()
	}
	*el.Logger() = logf.Log.WithName(name)
	return *el.Logger()
}

func LogAndSendEvent(el EventLogger, owner runtime.Object, reason, message string) {
	(*el.Logger()).Info(message)
	if *el.EventRecorder() != nil && owner != nil {
		(*el.EventRecorder()).Event(owner, corev1.EventTypeWarning, string(reason), message)
	}
}
