package context

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/http/curl"
	ispnClient "github.com/infinispan/infinispan-operator/pkg/infinispan/client"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ pipeline.Context = &contextImpl{}

func Provider(client client.Client, scheme *runtime.Scheme, kubernetes *kube.Kubernetes, eventRec record.EventRecorder) pipeline.ContextProvider {
	return &provider{
		Client:     client,
		scheme:     scheme,
		kubernetes: kubernetes,
		eventRec:   eventRec,
	}
}

type provider struct {
	client.Client
	scheme     *runtime.Scheme
	kubernetes *kube.Kubernetes
	eventRec   record.EventRecorder
}

func (p *provider) Get(ctx context.Context, config *pipeline.ContextProviderConfig) (pipeline.Context, error) {
	return &contextImpl{
		provider:              p,
		flowCtrl:              &flowCtrl{},
		ContextProviderConfig: config,
		infinispan:            config.Infinispan,
		ctx:                   ctx,
		versionManager:        config.VersionManager,
		ispnConfig:            &pipeline.ConfigFiles{},
		ispnClient:            nil,
		ispnPods:              nil,
		resources:             make(map[string]client.Object),
	}, nil
}

type contextImpl struct {
	*flowCtrl
	*provider
	*pipeline.ContextProviderConfig
	ctx            context.Context
	versionManager *version.Manager
	infinispan     *ispnv1.Infinispan
	ispnConfig     *pipeline.ConfigFiles
	ispnClient     api.Infinispan
	ispnPods       *corev1.PodList
	resources      map[string]client.Object
}

func (c *contextImpl) Operand() version.Operand {
	v := c.Infinispan.Spec.Version
	if v == "" {
		return c.versionManager.Latest()
	}
	if operand, err := c.VersionManager.WithRef(v); err != nil {
		panic(fmt.Errorf("spec.Version '%s' doesn't exist", v))
	} else {
		return operand
	}
}

func (c *contextImpl) Operands() *version.Manager {
	return c.VersionManager
}

func (c contextImpl) InfinispanClient() (api.Infinispan, error) {
	if c.ispnClient != nil {
		return c.ispnClient, nil
	}

	podList, err := c.InfinispanPods()
	if err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("unable to create Infinispan client, no Infinispan pods exists")
	}
	var pod string
	for _, p := range podList.Items {
		if kube.IsPodReady(p) {
			pod = p.Name
			break
		}
	}
	c.ispnClient = c.InfinispanClientForPod(pod)
	return c.ispnClient, nil
}

func (c contextImpl) InfinispanClientForPod(podName string) api.Infinispan {
	curlClient := c.curlClient(podName)
	return ispnClient.New(curlClient)
}

func (c contextImpl) InfinispanPods() (*corev1.PodList, error) {
	if c.ispnPods == nil {
		statefulSet := &appsv1.StatefulSet{}
		if err := c.Resources().Load(c.infinispan.GetStatefulSetName(), statefulSet); err != nil {
			err = fmt.Errorf("unable to list Infinispan pods as StatefulSet can't be loaded: %w", err)
			c.Requeue(err)
			return nil, err
		}

		podList := &corev1.PodList{}
		labels := c.infinispan.PodSelectorLabels()
		if err := c.Resources().List(labels, podList); err != nil {
			err = fmt.Errorf("unable to list Infinispan pods: %w", err)
			c.Requeue(err)
			return nil, err
		}
		kube.FilterPodsByOwnerUID(podList, statefulSet.GetUID())
		c.ispnPods = podList
	}
	return c.ispnPods.DeepCopy(), nil
}

func (c contextImpl) curlClient(podName string) *curl.Client {
	return curl.New(curl.Config{
		Credentials: &curl.Credentials{
			Username: c.ispnConfig.AdminIdentities.Username,
			Password: c.ispnConfig.AdminIdentities.Password,
		},
		Container: provision.InfinispanContainer,
		Podname:   podName,
		Namespace: c.infinispan.Namespace,
		Protocol:  "http",
		Port:      consts.InfinispanAdminPort,
	}, c.kubernetes)
}

func (c contextImpl) ConfigFiles() *pipeline.ConfigFiles {
	return c.ispnConfig
}

func (c contextImpl) Ctx() context.Context {
	return c.ctx
}

func (c contextImpl) Log() logr.Logger {
	return c.Logger
}

func (c contextImpl) EventRecorder() record.EventRecorder {
	return c.eventRec
}

func (c contextImpl) Kubernetes() *kube.Kubernetes {
	return c.kubernetes
}

func (c contextImpl) DefaultAnnotations() map[string]string {
	return c.ContextProviderConfig.DefaultAnnotations
}

func (c contextImpl) DefaultLabels() map[string]string {
	return c.ContextProviderConfig.DefaultLabels
}

func (c contextImpl) IsTypeSupported(gvk schema.GroupVersionKind) bool {
	_, ok := c.SupportedTypes[gvk]
	return ok
}

func (c contextImpl) UpdateInfinispan(updateFn func()) error {
	i := c.infinispan
	mutateFn := func() error {
		if i.CreationTimestamp.IsZero() || i.GetDeletionTimestamp() != nil {
			return errors.NewNotFound(schema.ParseGroupResource("infinispan.infinispan.org"), i.Name)
		}
		updateFn()
		return nil
	}
	_, err := c.Resources().CreateOrPatch(i, false, mutateFn, pipeline.RetryOnErr, pipeline.IgnoreNotFound)
	return err
}
