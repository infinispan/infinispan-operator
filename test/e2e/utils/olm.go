package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/operator-framework/api/pkg/manifests"
	coreosv1 "github.com/operator-framework/api/pkg/operators/v1"
	coreos "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func init() {
	PanicOnError(coreosv1.AddToScheme(Scheme))
	PanicOnError(coreos.AddToScheme(Scheme))
}

func (k TestKubernetes) CreateOperatorGroup(t *testing.T, name, namespace string, targetNamespaces ...string) {
	operatorGroup := &coreosv1.OperatorGroup{
		TypeMeta: metav1.TypeMeta{
			Kind: coreosv1.OperatorGroupKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: coreosv1.OperatorGroupSpec{
			TargetNamespaces: targetNamespaces,
		},
	}
	err := k.Kubernetes.Client.Create(context.TODO(), operatorGroup)
	require.NoError(t, err)
}

func (k TestKubernetes) DeleteOperatorGroup(t *testing.T, name, namespace string) {
	og := &coreosv1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k.Kubernetes.Client.Delete(context.TODO(), og, DeleteOpts...)
	ExpectMaybeNotFound(t, err)
}

func (k TestKubernetes) CreateSubscription(t *testing.T, sub *coreos.Subscription) {
	err := k.Kubernetes.Client.Create(context.TODO(), sub)
	require.NoError(t, err)
}

func (k TestKubernetes) DeleteSubscription(t *testing.T, name, namespace string) {
	sub := &coreos.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k.Kubernetes.Client.Delete(context.TODO(), sub, DeleteOpts...)
	ExpectMaybeNotFound(t, err)
}

func (k TestKubernetes) Subscription(t *testing.T, sub *coreos.Subscription) {
	err := k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: sub.Name, Namespace: sub.Namespace}, sub)
	require.NoError(t, err)
}

func (k TestKubernetes) UpdateSubscriptionChannel(t *testing.T, newChannel string, sub *coreos.Subscription) {
	retryOnConflict(t, func() error {
		k.Subscription(t, sub)
		sub.Spec.Channel = newChannel
		return k.Kubernetes.Client.Update(context.TODO(), sub)
	})
}

func (k TestKubernetes) InstallPlan(t *testing.T, sub *coreos.Subscription) *coreos.InstallPlan {
	ipRef := sub.Status.InstallPlanRef
	if ipRef == nil {
		return nil
	}
	installPlan := &coreos.InstallPlan{}

	err := k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: ipRef.Name, Namespace: ipRef.Namespace}, installPlan)
	require.NoError(t, err)
	return installPlan
}

func (k TestKubernetes) ApproveInstallPlan(t *testing.T, sub *coreos.Subscription) {
	retryOnConflict(t, func() error {
		installPlan := k.InstallPlan(t, sub)
		installPlan.Spec.Approved = true
		return k.Kubernetes.Client.Update(context.TODO(), installPlan)
	})
}

func (k TestKubernetes) InstalledCSV(t *testing.T, sub *coreos.Subscription) *coreos.ClusterServiceVersion {
	csvName := sub.Status.InstalledCSV
	csv := &coreos.ClusterServiceVersion{}
	err := k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: csvName, Namespace: sub.Namespace}, csv)
	require.NoError(t, err)
	return csv
}

func (k TestKubernetes) InstalledCSVServerImage(t *testing.T, sub *coreos.Subscription) string {
	csv := k.InstalledCSV(t, sub)
	envName := "RELATED_IMAGE_OPENJDK"
	envVars := csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env
	index := kubernetes.GetEnvVarIndex(envName, &envVars)
	if index < 0 {
		panic(fmt.Sprintf("Unable to find env var '%s' in CSV %s", envName, csv.Name))
	}
	return envVars[index].Value
}

func (k TestKubernetes) PackageManifest(packageName, catalogName string) (manifest *manifests.PackageManifest) {
	// The PackageManifest sometimes takes a while to be available, so we must poll until it's available
	err := wait.Poll(DefaultPollPeriod, MaxWaitTimeout, func() (done bool, err error) {
		type PackageManifestStatus struct {
			manifests.PackageManifest
		}

		type PackageManifest struct {
			Status PackageManifestStatus
		}

		type PackageManifestList struct {
			Items []PackageManifest
		}

		result := k.Kubernetes.RestClient.Get().
			AbsPath("/apis/packages.operators.coreos.com/v1/packagemanifests").
			Param("labelSelector", "catalog="+catalogName).
			Do(context.TODO())

		rawBytes, err := result.Raw()
		PanicOnError(err)
		manifestList := &PackageManifestList{}
		PanicOnError(json.Unmarshal(rawBytes, &manifestList))

		for _, p := range manifestList.Items {
			if p.Status.PackageName == packageName {
				manifest = &p.Status.PackageManifest
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		panic(fmt.Sprintf("Unable to retrieve '%s' PackageManifest, for catalog '%s'", packageName, catalogName))
	}
	return
}

func (k TestKubernetes) WaitForSubscriptionState(t *testing.T, state coreos.SubscriptionState, sub *coreos.Subscription) {
	k.WaitForSubscription(t, sub, func() bool {
		return sub.Status.State == state
	})
}

func (k TestKubernetes) WaitForSubscription(t *testing.T, sub *coreos.Subscription, predicate func() (done bool)) {
	err := wait.Poll(ConditionPollPeriod, ConditionWaitTimeout, func() (done bool, err error) {
		k.Subscription(t, sub)
		return predicate(), nil
	})
	require.NoError(t, err)
}

func retryOnConflict(t *testing.T, update func() error) {
	err := wait.Poll(DefaultPollPeriod, MaxWaitTimeout, func() (done bool, err error) {
		err = update()
		if err != nil {
			if k8serrors.IsConflict(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	require.NoError(t, err)
}
