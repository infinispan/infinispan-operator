package controllers

import (
	"fmt"

	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"k8s.io/utils/pointer"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const InfinispanListenerContainer = "infinispan-listener"

func (r *infinispanRequest) ReconcileConfigListener() error {
	if constants.ConfigListenerImageName == "" {
		err := fmt.Errorf("'%s' has not been defined", constants.ConfigListenerEnvName)
		r.log.Error(err, "unable to create ConfigListener deployment")
		return err
	}
	name := r.infinispan.GetConfigListenerName()
	namespace := r.infinispan.Namespace
	deployment := &appsv1.Deployment{}
	objectMeta := metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}

	listenerExists := r.Client.Get(r.ctx, types.NamespacedName{Namespace: namespace, Name: name}, deployment) == nil
	if listenerExists {
		container := GetContainer(InfinispanListenerContainer, &deployment.Spec.Template.Spec)
		if container != nil && container.Image == constants.ConfigListenerImageName {
			if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 0 {
				if err := r.ScaleConfigListener(1); err != nil {
					return err
				}
			}
			// The Deployment already exists with the expected image and number of replicas, do nothing
			return nil
		}
	}

	createOrUpdate := func(obj client.Object) error {
		if listenerExists {
			return r.Client.Update(r.ctx, obj)
		}
		err := controllerutil.SetControllerReference(r.infinispan, obj, r.scheme)
		if err != nil {
			return err
		}
		return r.Client.Create(r.ctx, obj)
	}

	// Create a ServiceAccount in the cluster namespace so that the ConfigListener has the required API permissions
	sa := &corev1.ServiceAccount{
		ObjectMeta: objectMeta,
	}
	if err := createOrUpdate(sa); err != nil {
		return err
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
				APIGroups: []string{v1.GroupVersion.Group},
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
		return err
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
		return err
	}

	// The deployment doesn't exist, create it
	labels := r.infinispan.PodLabels()
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
					Containers: []corev1.Container{
						{
							Name:  InfinispanListenerContainer,
							Image: constants.ConfigListenerImageName,
							Args: []string{
								"listener",
								"-namespace",
								namespace,
								"-cluster",
								r.infinispan.Name,
							},
						},
					},
					ServiceAccountName: name,
				},
			},
		},
	}
	return createOrUpdate(deployment)
}

func (r *infinispanRequest) DeleteConfigListener() error {
	objectMeta := metav1.ObjectMeta{
		Name:      r.infinispan.GetConfigListenerName(),
		Namespace: r.infinispan.Namespace,
	}

	ignoreNotFoundError := func(err error) error {
		if !errors.IsNotFound(err) {
			return err
		}
		return nil
	}

	deleteResource := func(obj client.Object) error {
		err := r.Client.Delete(r.ctx, obj)
		return ignoreNotFoundError(err)
	}

	if err := deleteResource(&appsv1.Deployment{ObjectMeta: objectMeta}); err != nil {
		return err
	}

	if err := deleteResource(&rbacv1.RoleBinding{ObjectMeta: objectMeta}); err != nil {
		return err
	}

	if err := deleteResource(&rbacv1.Role{ObjectMeta: objectMeta}); err != nil {
		return err
	}

	if err := deleteResource(&corev1.ServiceAccount{ObjectMeta: objectMeta}); err != nil {
		return err
	}

	// Remove any Cache CR instances owned by Infinispan as these were created by the Listener
	cacheList := &v2alpha1.CacheList{}
	if err := r.kubernetes.Client.List(r.ctx, cacheList, &client.ListOptions{Namespace: r.infinispan.Namespace}); err != nil {
		return fmt.Errorf("unable to rerieve existing Cache resources: %w", err)
	}

	// Iterate over all existing CRs, marking for deletion any that do not have a cache definition on the server
	for _, cache := range cacheList.Items {
		if kube.IsOwnedBy(&cache, r.infinispan) {
			cache.ObjectMeta.Annotations[constants.ListenerAnnotationDelete] = "true"
			if err := r.kubernetes.Client.Update(r.ctx, &cache); err != nil {
				return ignoreNotFoundError(err)
			}
		}
	}
	return nil
}

func (r *infinispanRequest) ScaleConfigListener(replicas int32) error {
	i := r.infinispan
	if !i.IsConfigListenerEnabled() {
		return nil
	}
	// Remove the ConfigListener deployment as no Infinispan Pods exist
	r.log.Info("Scaling ConfigListener deployment", "replicas", replicas)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      i.GetConfigListenerName(),
			Namespace: i.Namespace,
		},
	}

	_, err := controllerutil.CreateOrPatch(r.ctx, r.Client, deployment, func() error {
		if deployment.CreationTimestamp.IsZero() {
			return errors.NewNotFound(appsv1.Resource("deployment"), deployment.Name)
		}
		deployment.Spec.Replicas = pointer.Int32Ptr(replicas)
		return nil
	})

	if err != nil {
		r.log.Error(err, "unable to scale ConfigListener Deployment")
		return err
	}
	return nil
}
