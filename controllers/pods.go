package controllers

import (
	"context"

	infinispanv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// PodsCreatedBy Obtain pods created by a certain statefulSet
func PodsCreatedBy(namespace string, kube *kubernetes.Kubernetes, ctx context.Context, statefulSetName string) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	err := kube.ResourcesList(namespace, map[string]string{consts.StatefulSetPodLabel: statefulSetName}, podList, ctx)
	if err != nil {
		return podList, err
	}
	return podList, nil
}

// PodList Obtain list of pods associated with the supplied Infinispan cluster
func PodList(infinispan *infinispanv1.Infinispan, kube *kubernetes.Kubernetes, ctx context.Context) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	stateFulSet := &appsv1.StatefulSet{}
	namespace := infinispan.GetNamespace()
	err := kube.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: infinispan.GetStatefulSetName()}, stateFulSet)
	if err != nil {
		return podList, nil
	}
	// Obtain pod list associated with the cluster
	err = kube.ResourcesList(namespace, infinispan.PodSelectorLabels(), podList, ctx)
	if err != nil {
		return nil, err
	}

	// Filter out pods not owned by the statefulSet
	ownerId := stateFulSet.GetUID()
	pos := 0
	for _, item := range podList.Items {
		for _, reference := range item.GetOwnerReferences() {
			if reference.UID == ownerId {
				podList.Items[pos] = item
				pos++
			}
		}
	}
	podList.Items = podList.Items[:pos]
	return podList, nil
}
