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

func Logger(el EventLogger, name string) logr.Logger {
	if *el.Logger() != nil {
		return *el.Logger()
	}
	*el.Logger() = logf.Log.WithName(name)
	return *el.Logger()
}

func LogAndSendEvent(el EventLogger, owner runtime.Object, reason, message string, keysAndValues ...interface{}) {
	(*el.Logger()).Info(message)
	if *el.EventRecorder() != nil && owner != nil {
		(*el.EventRecorder()).Event(owner, corev1.EventTypeWarning, string(reason), message)
	}
}
