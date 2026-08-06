package controllers

import (
	"context"

	"github.com/go-logr/logr"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Pausable interface {
	client.Object
	SetPausedCondition(message string) bool
	RemovePausedCondition() bool
}

func HandleReconciliationPause(
	ctx context.Context,
	obj Pausable,
	statusClient client.StatusClient,
	eventRec record.EventRecorder,
	logger logr.Logger,
) (paused bool, err error) {
	if obj.GetAnnotations()[consts.AnnotationPaused] == "true" {
		logger.Info("Reconciliation paused via annotation 'infinispan.org/paused'")
		if obj.SetPausedCondition(consts.ConditionMessageReconciliationPaused) {
			if err := statusClient.Status().Update(ctx, obj); err != nil {
				return false, err
			}
		}
		eventRec.Event(obj, corev1.EventTypeNormal, consts.EventReasonReconciliationPaused, consts.EventMessageReconciliationPaused)
		return true, nil
	}

	if obj.RemovePausedCondition() {
		if err := statusClient.Status().Update(ctx, obj); err != nil {
			return false, err
		}
	}
	return false, nil
}
