package provision

import (
	"fmt"
	"reflect"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const InfinispanListenerContainer = "infinispan-listener"

func ConfigListener(i *ispnv1.Infinispan, ctx pipeline.Context) {
	if !i.IsConfigListenerEnabled() {
		RemoveConfigListener(i, ctx)
		return
	}

	var configListenerImage string
	if constants.ConfigListenerImageName != "" {
		configListenerImage = constants.ConfigListenerImageName
	} else {
		// If env not explicitly set, use the Operator image
		if image, err := kube.GetOperatorImage(ctx.Ctx(), ctx.Kubernetes().Client); err != nil {
			ctx.Log().Error(err, "unable to create ConfigListener deployment")
			ctx.Requeue(err)
			return
		} else {
			configListenerImage = image
		}
	}

	r := ctx.Resources()
	name := i.GetConfigListenerName()
	namespace := i.Namespace

	objectMeta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}

	deployment := &appsv1.Deployment{}
	listenerExists := r.Load(name, deployment) == nil
	if listenerExists {
		container := kube.GetContainer(InfinispanListenerContainer, &deployment.Spec.Template.Spec)
		if container != nil && container.Image == configListenerImage {
			if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 0 {
				if err := ScaleConfigListener(1, i, ctx); err != nil {
					ctx.Requeue(err)
					return
				}
			}

			// Update the deployment args if the ConfigListener log level has been updated
			logLevel := string(i.Spec.ConfigListener.Logging.Level)
			if container.Args[len(container.Args)-1] != logLevel {
				err := UpdateConfigListenerDeployment(i, ctx, func(deployment *appsv1.Deployment) {
					container = kube.GetContainer(InfinispanListenerContainer, &deployment.Spec.Template.Spec)
					container.Args[len(container.Args)-1] = logLevel
				})

				if err != nil {
					ctx.Requeue(fmt.Errorf("unable to update ConfigListener log level: %w", err))
					return
				}
			}

			if podResources, err := podResources(i); err != nil {
				ctx.Requeue(fmt.Errorf("unable to calculate ConfigListener pod resources on update: %w", err))
				return
			} else if podResources != nil && !reflect.DeepEqual(*podResources, container.Resources) {
				err := UpdateConfigListenerDeployment(i, ctx, func(deployment *appsv1.Deployment) {
					container = kube.GetContainer(InfinispanListenerContainer, &deployment.Spec.Template.Spec)
					container.Resources = *podResources
				})

				if err != nil {
					ctx.Requeue(fmt.Errorf("unable to update ConfigListener resources: %w", err))
					return
				}
			}

			// The Deployment already exists with the expected spec, do nothing
			return
		}
	}

	createOrUpdate := func(obj client.Object) error {
		err := r.Load(obj.GetName(), obj)
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("unable to create ConfigListener resource: %w", err)
		}

		if errors.IsNotFound(err) {
			return r.Create(obj, true, pipeline.RetryOnErr)
		} else {
			return r.Update(obj, pipeline.RetryOnErr)
		}
	}
	// Create a ServiceAccount in the cluster namespace so that the ConfigListener has the required API permissions
	sa := &corev1.ServiceAccount{
		ObjectMeta: objectMeta,
	}
	if err := createOrUpdate(sa); err != nil {
		return
	}

	role := &rbacv1.Role{
		ObjectMeta: objectMeta,
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{v2alpha1.GroupVersion.Group},
				Resources: []string{"caches"},
				Verbs: []string{
					"create",
					"delete",
					"get",
					"list",
					"patch",
					"update",
					"watch",
				},
			},
			{
				APIGroups: []string{ispnv1.GroupVersion.Group},
				Resources: []string{"infinispans"},
				Verbs:     []string{"get"},
			}, {
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"list"},
			}, {
				APIGroups: []string{""},
				Resources: []string{"pods/exec"},
				Verbs:     []string{"create"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get"},
			},
		},
	}
	if err := createOrUpdate(role); err != nil {
		return
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: objectMeta,
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     name,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      name,
			Namespace: namespace,
		}},
	}
	if err := createOrUpdate(roleBinding); err != nil {
		return
	}

	operandVersions, err := ctx.Operands().Json()
	if err != nil {
		ctx.Requeue(fmt.Errorf("unable to marshall Operands for ConfigListener deployment: %w", err))

	}

	container := &corev1.Container{
		Name:  InfinispanListenerContainer,
		Image: configListenerImage,
		Args: []string{
			"listener",
			"-namespace",
			namespace,
			"-cluster",
			i.Name,
			"-zap-log-level",
			string(i.Spec.ConfigListener.Logging.Level),
		},
		Env: []corev1.EnvVar{{Name: "INFINISPAN_OPERAND_VERSIONS", Value: operandVersions}},
	}

	if podResources, err := podResources(i); err != nil {
		ctx.Requeue(fmt.Errorf("unable to calculate ConfigListener pod resources: %w", err))
	} else if podResources != nil {
		container.Resources = *podResources
	}

	// The deployment doesn't exist, create it
	labels := i.PodLabels()
	labels["app"] = "infinispan-config-listener-pod"
	deployment = &appsv1.Deployment{
		ObjectMeta: objectMeta,
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers:         []corev1.Container{*container},
					ServiceAccountName: name,
				},
			},
		},
	}
	if err := createOrUpdate(deployment); err != nil {
		return
	}
}

func podResources(i *ispnv1.Infinispan) (*corev1.ResourceRequirements, error) {
	spec := i.Spec.ConfigListener
	if spec.CPU == "" && spec.Memory == "" {
		return nil, nil
	}

	req := &corev1.ResourceRequirements{
		Limits:   corev1.ResourceList{},
		Requests: corev1.ResourceList{},
	}
	if spec.Memory != "" {
		memRequests, memLimits, err := spec.MemoryResources()
		if err != nil {
			return req, err
		}
		req.Requests[corev1.ResourceMemory] = memRequests
		req.Limits[corev1.ResourceMemory] = memLimits
	}

	if spec.CPU != "" {
		cpuRequests, cpuLimits, err := spec.CpuResources()
		if err != nil {
			return req, err
		}
		req.Requests[corev1.ResourceCPU] = cpuRequests
		req.Limits[corev1.ResourceCPU] = cpuLimits
	}
	return req, nil
}

func RemoveConfigListener(i *ispnv1.Infinispan, ctx pipeline.Context) {
	resources := []client.Object{
		&appsv1.Deployment{},
		&rbacv1.Role{},
		&rbacv1.RoleBinding{},
		&corev1.ServiceAccount{},
	}

	name := i.GetConfigListenerName()
	for _, obj := range resources {
		if err := ctx.Resources().Delete(name, obj, pipeline.RetryOnErr, pipeline.IgnoreNotFound); err != nil {
			return
		}
	}

	// Remove any Cache CR instances owned by Infinispan as these were created by the Listener
	cacheList := &v2alpha1.CacheList{}
	if err := ctx.Resources().List(nil, cacheList); err != nil {
		ctx.Requeue(fmt.Errorf("unable to retrieve existing Cache resources: %w", err))
		return
	}
	// Iterate over all existing CRs, marking for deletion any that do not have a cache definition on the server
	for _, cache := range cacheList.Items {
		if kube.IsOwnedBy(&cache, i) {
			cache.ObjectMeta.Annotations[constants.ListenerAnnotationDelete] = "true"
			if err := ctx.Resources().Update(&cache, pipeline.IgnoreNotFound); err != nil {
				ctx.Requeue(fmt.Errorf("unable to mark Cache CR '%s' for deletion: %w", cache.Name, err))
				return
			}
		}
	}
}

func ScaleConfigListener(replicas int32, i *ispnv1.Infinispan, ctx pipeline.Context) error {
	if !i.IsConfigListenerEnabled() {
		return nil
	}
	// Remove the ConfigListener deployment as no Infinispan Pods exist
	ctx.Log().Info("Scaling ConfigListener deployment", "replicas", replicas)

	err := UpdateConfigListenerDeployment(i, ctx, func(deployment *appsv1.Deployment) {
		deployment.Spec.Replicas = pointer.Int32Ptr(replicas)
	})

	if err != nil {
		ctx.Log().Error(err, "unable to scale ConfigListener Deployment")
	}
	return err
}

func UpdateConfigListenerDeployment(i *ispnv1.Infinispan, ctx pipeline.Context, mutate func(deployment *appsv1.Deployment)) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      i.GetConfigListenerName(),
			Namespace: i.Namespace,
		},
	}

	_, err := ctx.Resources().CreateOrPatch(deployment, false, func() error {
		if deployment.CreationTimestamp.IsZero() {
			return errors.NewNotFound(appsv1.Resource("deployment"), deployment.Name)
		}
		mutate(deployment)
		return nil
	})
	return err
}
