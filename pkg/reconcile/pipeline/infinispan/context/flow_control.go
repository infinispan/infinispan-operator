package context

import (
	"time"

	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
)

type flowCtrl struct {
	retry bool
	stop  bool
	err   error
	delay time.Duration
}

func (f *flowCtrl) FlowStatus() pipeline.FlowStatus {
	return pipeline.FlowStatus{
		Retry: f.retry,
		Stop:  f.stop,
		Err:   f.err,
		Delay: f.delay,
	}
}

func (f *flowCtrl) Requeue(err error) {
	f.RequeueAfter(0, err)
}

func (f *flowCtrl) RequeueAfter(delay time.Duration, err error) {
	f.retry = true
	f.delay = delay
	f.Stop(err)
}

func (f *flowCtrl) RequeueEventually(delay time.Duration) {
	f.retry = true
	f.delay = delay
}

func (f *flowCtrl) Stop(err error) {
	f.stop = true
	f.err = err
}
