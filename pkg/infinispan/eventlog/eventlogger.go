package eventlog

import (
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
)

type EventLogger interface {
	InitLogger(keysAndValues ...interface{})
	Logger() logr.Logger
	LogAndSendEvent(owner runtime.Object, message, reason string)
}
