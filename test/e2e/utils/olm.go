package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/infinispan/infinispan-operator/controllers/constants"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/operator-framework/api/pkg/manifests"
	coreosv1 "github.com/operator-framework/api/pkg/operators/v1"
	coreos "github.com/operator-framework/api/pkg/operators/v1alpha1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func init() {
	ExpectNoError(coreosv1.AddToScheme(Scheme))
	ExpectNoError(coreos.AddToScheme(Scheme))
}

type OLMEnv struct {
	CatalogSource          string
	CatalogSourceNamespace string
	SubName                string
	SubNamespace           string
	SubPackage             string
	SubStartingCSV         string

	PackageManifest *manifests.PackageManifest
	SourceChannel   manifests.PackageChannel
	TargetChannel   manifests.PackageChannel
}

func (env OLMEnv) PrintManifest() {
	bytes, err := yaml.Marshal(env.PackageManifest)
	ExpectNoError(err)
	fmt.Println(string(bytes))
	fmt.Println("Source channel: " + env.SourceChannel.Name)
	fmt.Println("Target channel: " + env.TargetChannel.Name)
	fmt.Println("Starting CSV: " + env.SubStartingCSV)
}

func (k TestKubernetes) OLMTestEnv() OLMEnv {
	env := &OLMEnv{
		CatalogSource:          constants.GetEnvWithDefault("SUBSCRIPTION_CATALOG_SOURCE", "test-catalog"),
		CatalogSourceNamespace: constants.GetEnvWithDefault("SUBSCRIPTION_CATALOG_SOURCE_NAMESPACE", Namespace),
		SubName:                constants.GetEnvWithDefault("SUBSCRIPTION_NAME", "infinispan-operator"),
		SubNamespace:           constants.GetEnvWithDefault("SUBSCRIPTION_NAMESPACE", Namespace),
		SubPackage:             constants.GetEnvWithDefault("SUBSCRIPTION_PACKAGE", "infinispan"),
	}
	packageManifest := k.PackageManifest(env.SubPackage, env.CatalogSource)
	env.PackageManifest = packageManifest
	env.SourceChannel = getChannel("SUBSCRIPTION_CHANNEL_SOURCE", packageManifest)
	env.TargetChannel = getChannel("SUBSCRIPTION_CHANNEL_TARGET", packageManifest)
	env.SubStartingCSV = constants.GetEnvWithDefault("SUBSCRIPTION_STARTING_CSV", env.SourceChannel.CurrentCSVName)
	return *env
}

// Utilise the provided env variable for the channel string if it exists, otherwise retrieve the channel from the PackageManifest
func getChannel(env string, manifest *manifests.PackageManifest) manifests.PackageChannel {
	channels := manifest.Channels

	// If an env variable exists, then return the PackageChannel with that name
	if env, exists := os.LookupEnv(env); exists {
		for _, channel := range channels {
			if channel.Name == env {
				return channel
			}
		}
		panic(fmt.Errorf("unable to find channel with name '%s' in PackageManifest", env))
	}

	for _, channel := range channels {
		if channel.Name == manifest.DefaultChannelName {
			return channel
		}
	}
	return manifests.PackageChannel{}
}

func (k TestKubernetes) CreateSubscriptionAndApproveInitialVersion(olm OLMEnv, sub *coreos.Subscription) {
	// Create OperatorGroup only if Subscription is created in non-global namespace
	if olm.SubNamespace != "openshift-operators" && olm.SubNamespace != "operators" {
		k.CreateOperatorGroup(olm.SubName, olm.SubNamespace, olm.SubNamespace)
	}
	k.CreateSubscription(sub)
	// Approve the initial startingCSV InstallPlan
	k.WaitForSubscriptionState(coreos.SubscriptionStateUpgradePending, sub)
	k.ApproveInstallPlan(sub)

	k.WaitForCrd(&apiextv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "infinispans.infinispan.org",
		},
	})
	k.WaitForPods(1, ConditionWaitTimeout, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{"app.kubernetes.io/name": "infinispan-operator"}),
	}, nil)
}

func (k TestKubernetes) CleanupOLMTest(t *testing.T, testIdentifier, subName, subNamespace, subPackage string) {
	panicVal := recover()

	cleanupOlm := func() {
		opts := []client.DeleteAllOfOption{
			client.InNamespace(subNamespace),
			client.MatchingLabels(
				map[string]string{
					fmt.Sprintf("operators.coreos.com/%s.%s", subPackage, subNamespace): "",
				},
			),
		}

		testFailed := t != nil && t.Failed()
		if panicVal != nil || testFailed {
			dir := fmt.Sprintf("%s/%s", LogOutputDir, testIdentifier)

			if t == nil {
				// Only remove existing dir if test doesn't exist, as otherwise the output directory will be initialized
				// by the call to CleanNamespaceAndLogWithPanic which is executed before this function
				err := os.RemoveAll(dir)
				LogError(err)

				err = os.MkdirAll(dir, os.ModePerm)
				LogError(err)
			}

			k.WriteAllResourcesToFile(dir, subNamespace, "OperatorGroup", &coreosv1.OperatorGroupList{}, map[string]string{})
			k.WriteAllResourcesToFile(dir, subNamespace, "Subscription", &coreos.SubscriptionList{}, map[string]string{})
			k.WriteAllResourcesToFile(dir, subNamespace, "ClusterServiceVersion", &coreos.ClusterServiceVersionList{}, map[string]string{})
			k.WriteAllResourcesToFile(dir, subNamespace, "Deployment", &appsv1.DeploymentList{}, map[string]string{})
			// Print 2.1.x Operator pod logs
			k.WriteAllResourcesToFile(dir, subNamespace, "Pod", &corev1.PodList{}, map[string]string{"name": "infinispan-operator"})
			// Print latest Operator logs
			k.WriteAllResourcesToFile(dir, subNamespace, "Pod", &corev1.PodList{}, map[string]string{"app.kubernetes.io/name": "infinispan-operator"})
		}

		// Cleanup OLM resources
		k.DeleteSubscription(subName, subNamespace)
		k.DeleteOperatorGroup(subName, subNamespace)

		operatorDeployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "infinispan-operator-controller-manager",
				Namespace: subNamespace,
			},
		}
		k.DeleteResource(subNamespace, labels.SelectorFromSet(map[string]string{"control-plane": "controller-manager"}), operatorDeployment, SinglePodTimeout)

		ExpectMaybeNotFound(k.Kubernetes.Client.DeleteAllOf(context.TODO(), &coreos.ClusterServiceVersion{}, opts...))
	}
	// We must cleanup OLM resources after any Infinispan CRs etc, otherwise the CRDs may have been removed from the cluster
	defer cleanupOlm()

	// Cleanup Infinispan resources
	if t != nil {
		k.CleanNamespaceAndLogWithPanic(t, Namespace, panicVal)
	}
}

func (k TestKubernetes) CreateOperatorGroup(name, namespace string, targetNamespaces ...string) {
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
	ExpectNoError(err)
}

func (k TestKubernetes) DeleteOperatorGroup(name, namespace string) {
	og := &coreosv1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k.Kubernetes.Client.Delete(context.TODO(), og, DeleteOpts...)
	ExpectMaybeNotFound(err)
}

func (k TestKubernetes) CreateSubscription(sub *coreos.Subscription) {
	err := k.Kubernetes.Client.Create(context.TODO(), sub)
	ExpectNoError(err)
}

func (k TestKubernetes) DeleteSubscription(name, namespace string) {
	sub := &coreos.Subscription{}

	if err := k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, sub); k8serrors.IsNotFound(err) {
		return
	}

	if csv, err := k.InstalledCSV(sub); err == nil {
		ExpectMaybeNotFound(k.Kubernetes.Client.Delete(context.TODO(), csv, DeleteOpts...))
	}
	ExpectMaybeNotFound(k.Kubernetes.Client.Delete(context.TODO(), sub, DeleteOpts...))
}

func (k TestKubernetes) Subscription(sub *coreos.Subscription) {
	err := k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: sub.Name, Namespace: sub.Namespace}, sub)
	ExpectNoError(err)
}

func (k TestKubernetes) UpdateSubscriptionChannel(newChannel string, sub *coreos.Subscription) {
	retryOnConflict(func() error {
		k.Subscription(sub)
		sub.Spec.Channel = newChannel
		return k.Kubernetes.Client.Update(context.TODO(), sub)
	})
}

func (k TestKubernetes) InstallPlan(sub *coreos.Subscription) *coreos.InstallPlan {
	ipRef := sub.Status.InstallPlanRef
	if ipRef == nil {
		return nil
	}
	installPlan := &coreos.InstallPlan{}

	err := k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: ipRef.Name, Namespace: ipRef.Namespace}, installPlan)
	ExpectNoError(err)
	return installPlan
}

func (k TestKubernetes) ApproveInstallPlan(sub *coreos.Subscription) {
	retryOnConflict(func() error {
		installPlan := k.InstallPlan(sub)
		installPlan.Spec.Approved = true
		return k.Kubernetes.Client.Update(context.TODO(), installPlan)
	})
}

func (k TestKubernetes) InstalledCSV(sub *coreos.Subscription) (*coreos.ClusterServiceVersion, error) {
	csvName := sub.Status.InstalledCSV
	csv := &coreos.ClusterServiceVersion{}
	err := k.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: csvName, Namespace: sub.Namespace}, csv)
	return csv, err
}

func (k TestKubernetes) InstalledCSVEnv(envName string, sub *coreos.Subscription) string {
	csv, err := k.InstalledCSV(sub)
	ExpectNoError(err)
	envVars := csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env
	index := kubernetes.GetEnvVarIndex(envName, &envVars)
	if index < 0 {
		return ""
	}
	return envVars[index].Value
}

func (k TestKubernetes) SetRelatedImagesEnvs(sub *coreos.Subscription) {
	csv, err := k.InstalledCSV(sub)
	ExpectNoError(err)

	envVars := csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env
	for _, envVar := range envVars {
		if strings.HasPrefix(envVar.Name, "RELATED") {
			os.Setenv(envVar.Name, envVar.Value)
		}
	}
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
		ExpectNoError(err)
		manifestList := &PackageManifestList{}
		ExpectNoError(json.Unmarshal(rawBytes, &manifestList))

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

func (k TestKubernetes) WaitForSubscriptionState(state coreos.SubscriptionState, sub *coreos.Subscription) {
	k.WaitForSubscription(sub, func() bool {
		return sub.Status.State == state
	})
}

func (k TestKubernetes) WaitForSubscription(sub *coreos.Subscription, predicate func() (done bool)) {
	err := wait.Poll(ConditionPollPeriod, ConditionWaitTimeout, func() (done bool, err error) {
		k.Subscription(sub)
		return predicate(), nil
	})
	ExpectNoError(err)
}

func (k TestKubernetes) WaitForCSVSucceeded(sub *coreos.Subscription) {

	err := wait.Poll(ConditionPollPeriod, ConditionWaitTimeout, func() (done bool, err error) {
		csv, _ := k.InstalledCSV(sub)
		return csv.Status.Phase == "Succeeded", nil
	})
	ExpectNoError(err)
}

func retryOnConflict(update func() error) {
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
	ExpectNoError(err)
}
