package controllers

import (
	"fmt"

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
			// The Deployment already exists with the expected image, do nothing
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
	labels := ConfigListenerPodLabels(r.infinispan.Name)
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

	delete := func(obj client.Object) error {
		err := r.Client.Delete(r.ctx, obj)
		if !errors.IsNotFound(err) {
			return err
		}
		return nil
	}

	if err := delete(&appsv1.Deployment{ObjectMeta: objectMeta}); err != nil {
		return err
	}

	if err := delete(&rbacv1.RoleBinding{ObjectMeta: objectMeta}); err != nil {
		return err
	}

	if err := delete(&rbacv1.Role{ObjectMeta: objectMeta}); err != nil {
		return err
	}
	return delete(&corev1.ServiceAccount{ObjectMeta: objectMeta})
}
