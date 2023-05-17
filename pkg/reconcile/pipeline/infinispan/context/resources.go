package context

import (
	"fmt"
	"reflect"

	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	k8sctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type resources struct {
	*contextImpl
}

func (c *contextImpl) Resources() pipeline.Resources {
	return &resources{c}
}

func (r resources) Create(obj client.Object, setControllerRef bool, opts ...func(config *pipeline.ResourcesConfig)) error {
	if setControllerRef {
		if err := r.SetControllerReference(obj); err != nil {
			return err
		}
	}
	err := r.Client.Create(r.ctx, obj)
	return r.createOrMutateErr(err, opts...)
}

func (r resources) CreateOrUpdate(obj client.Object, setControllerRef bool, mutate func() error, opts ...func(config *pipeline.ResourcesConfig)) (pipeline.OperationResult, error) {
	res, err := k8sctrlutil.CreateOrUpdate(r.ctx, r.Client, obj, func() error {
		if mutate != nil {
			if err := mutate(); err != nil {
				return err
			}
		}
		if setControllerRef {
			return r.SetControllerReference(obj)
		}
		return nil
	})
	return pipeline.OperationResult(res), r.createOrMutateErr(err, opts...)
}

func (r resources) CreateOrPatch(obj client.Object, setControllerRef bool, mutate func() error, opts ...func(config *pipeline.ResourcesConfig)) (pipeline.OperationResult, error) {
	res, err := kube.CreateOrPatch(r.ctx, r.Client, obj, func() error {
		if mutate != nil {
			if err := mutate(); err != nil {
				return err
			}
		}
		if setControllerRef {
			return r.SetControllerReference(obj)
		}
		return nil
	})
	return pipeline.OperationResult(res), r.createOrMutateErr(err, opts...)
}

func (r resources) createOrMutateErr(err error, opts ...func(config *pipeline.ResourcesConfig)) error {
	if err != nil {
		config := resourcesConfig(opts...)
		isNotFound := errors.IsNotFound(err)
		if isNotFound && config.IgnoreNotFound {
			return nil
		}

		if config.RetryOnErr {
			r.Requeue(err)
		}
		return err
	}
	return nil
}

func (r resources) Delete(name string, obj client.Object, opts ...func(config *pipeline.ResourcesConfig)) error {
	obj.SetName(name)
	obj.SetNamespace(r.infinispan.Namespace)
	err := r.Client.Delete(r.ctx, obj)
	return r.createOrMutateErr(err, append(opts, pipeline.IgnoreNotFound)...)
}

func (r resources) List(set map[string]string, list client.ObjectList, opts ...func(config *pipeline.ResourcesConfig)) error {
	labelSelector := labels.SelectorFromSet(set)
	listOps := &client.ListOptions{Namespace: r.infinispan.Namespace, LabelSelector: labelSelector}
	config := resourcesConfig(opts...)
	err := r.Client.List(r.ctx, list, listOps)
	if err != nil && config.RetryOnErr {
		r.Requeue(err)
	}
	return err
}

func (r resources) Load(name string, obj client.Object, opts ...func(config *pipeline.ResourcesConfig)) error {
	obj.SetName(name)
	obj.SetNamespace(r.infinispan.Namespace)

	loadFn := func() error {
		return r.Client.Get(r.ctx, types.NamespacedName{Namespace: r.infinispan.Namespace, Name: name}, obj)
	}
	return r.load(name, obj, loadFn, opts...)
}

func (r resources) LoadGlobal(name string, obj client.Object, opts ...func(config *pipeline.ResourcesConfig)) error {
	loadFn := func() error {
		return r.Client.Get(r.ctx, types.NamespacedName{Name: name}, obj)
	}
	return r.load(name, obj, loadFn, opts...)
}

func (r resources) load(name string, obj client.Object, load func() error, opts ...func(config *pipeline.ResourcesConfig)) error {
	config := resourcesConfig(opts...)
	handleErr := func(err error) error {
		if err == nil {
			return nil
		}

		isNotFound := errors.IsNotFound(err)
		if isNotFound && config.IgnoreNotFound {
			return nil
		}

		if config.RetryOnErr {
			// Set NotFound errors to nil so that the Operator logs are not populated with unuseful NotFound stacktraces
			r.Requeue(client.IgnoreNotFound(err))
		}

		if isNotFound && !config.SkipEventRec {
			msg := fmt.Sprintf("%s resource '%s' not ready", reflect.TypeOf(obj).Elem().Name(), name)
			r.Log().Info(msg)
			r.EventRecorder().Event(r.infinispan, corev1.EventTypeWarning, "ResourceNotReady", msg)
		}
		return err
	}

	gvk, err := apiutil.GVKForObject(obj, r.scheme)
	if err != nil {
		return fmt.Errorf("unable to get the GVK for object: %w", err)
	}
	key := fmt.Sprintf("%s.%s", name, gvk)

	if !config.InvalidateCache {
		if storedObj, ok := r.resources[key]; ok {
			// Reflection trickery so that the passed obj reference is updated to the stored pointer
			reflect.ValueOf(obj).Elem().Set(reflect.ValueOf(storedObj).Elem())
			return nil
		}
	}
	if err := load(); err != nil {
		return handleErr(err)
	}
	r.resources[key] = obj
	return nil
}

func (r resources) SetControllerReference(controlled metav1.Object) error {
	return k8sctrlutil.SetControllerReference(r.infinispan, controlled, r.scheme)
}

func (r resources) Update(obj client.Object, opts ...func(config *pipeline.ResourcesConfig)) error {
	err := r.Client.Update(r.ctx, obj)
	return r.createOrMutateErr(err, opts...)
}

func resourcesConfig(opts ...func(config *pipeline.ResourcesConfig)) *pipeline.ResourcesConfig {
	config := &pipeline.ResourcesConfig{}
	for _, opt := range opts {
		opt(config)
	}
	return config
}
