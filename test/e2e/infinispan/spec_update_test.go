package infinispan

import (
	"context"
	"fmt"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
)

// Test if spec.container.cpu update is handled
func TestContainerCPUUpdateWithTwoReplicas(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.CPU = "900m:550m"
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		limit := resource.MustParse("900m")
		request := resource.MustParse("550m")
		if limit.Cmp(ss.Spec.Template.Spec.Containers[0].Resources.Limits["cpu"]) != 0 ||
			request.Cmp(ss.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"]) != 0 {
			panic("CPU field not updated")
		}
	}
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Service.Container.EphemeralStorage = false
	})
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

// Test if spec.container.memory update is handled
func TestContainerMemoryUpdate(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.Memory = "512Mi:256Mi"
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		limit := resource.MustParse("512Mi")
		request := resource.MustParse("256Mi")
		if limit.Cmp(ss.Spec.Template.Spec.Containers[0].Resources.Limits["memory"]) != 0 ||
			request.Cmp(ss.Spec.Template.Spec.Containers[0].Resources.Requests["memory"]) != 0 {
			panic("Memory field not updated")
		}
	}
	spec := tutils.DefaultSpec(t, testKube, nil)
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

func TestContainerJavaOptsUpdate(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.ExtraJvmOpts = "-XX:NativeMemoryTracking=summary"
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		env := ss.Spec.Template.Spec.Containers[0].Env
		for _, value := range env {
			if value.Name == "JAVA_OPTIONS" {
				if value.Value != "-XX:NativeMemoryTracking=summary" {
					panic("JAVA_OPTIONS not updated")
				} else {
					return
				}
			}
		}
		panic("JAVA_OPTIONS not updated")
	}
	spec := tutils.DefaultSpec(t, testKube, nil)
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

func TestUpdatePodTargetLabels(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	podLabel := "pod-label-1"
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.ObjectMeta.Annotations = map[string]string{
			ispnv1.PodTargetLabels: podLabel,
		}
		ispn.ObjectMeta.Labels[podLabel] = "value"
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		labels := ss.Spec.Template.ObjectMeta.Labels
		if _, exists := labels[podLabel]; !exists {
			panic(fmt.Sprintf("'%s' label not found in StatefulSet spec", podLabel))
		}
	}
	spec := tutils.DefaultSpec(t, testKube, nil)
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

func TestProbeUpdates(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Service.Container.LivenessProbe.TimeoutSeconds = pointer.Int32(1000)
		ispn.Spec.Service.Container.ReadinessProbe.TimeoutSeconds = pointer.Int32(1000)
		ispn.Spec.Service.Container.StartupProbe.TimeoutSeconds = pointer.Int32(1000)
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		verify := func(val int32) {
			if val != 1000 {
				panic("Probe values not updated")
			}
		}
		c := ss.Spec.Template.Spec.Containers[0]
		verify(c.ReadinessProbe.TimeoutSeconds)
		verify(c.LivenessProbe.TimeoutSeconds)
		verify(c.StartupProbe.TimeoutSeconds)
	}
	spec := tutils.DefaultSpec(t, testKube, nil)
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

func TestXSiteUpdates(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	spec := tutils.DefaultSpec(t, testKube, nil)
	transport, router, trust := tutils.CreateDefaultCrossSiteKeyAndTrustStore()
	keystoreSecret := xsiteTlsSecret(spec.Name+"-transport", spec.Namespace, consts.DefaultSiteKeyStoreFileName, transport)
	routerSecret := xsiteTlsSecret(spec.Name+"-router", spec.Namespace, consts.DefaultSiteKeyStoreFileName, router)
	trustSecret := xsiteTlsSecret(spec.Name+"-trust", spec.Namespace, consts.DefaultSiteTrustStoreFileName, trust)

	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Service.Sites = &ispnv1.InfinispanSitesSpec{
			Local: ispnv1.InfinispanSitesLocalSpec{
				Name: "local",
				Expose: ispnv1.CrossSiteExposeSpec{
					Type: ispnv1.CrossSiteExposeTypeClusterIP,
				},
				MaxRelayNodes: 1,
				Discovery: &ispnv1.DiscoverySiteSpec{
					Memory: "500Mi",
					CPU:    "500m",
				},
				Encryption: &ispnv1.EncryptionSiteSpec{
					TransportKeyStore: ispnv1.CrossSiteKeyStore{
						SecretName: keystoreSecret.Name,
					},
					TrustStore: &ispnv1.CrossSiteTrustStore{
						SecretName: trustSecret.Name,
					},
					RouterKeyStore: ispnv1.CrossSiteKeyStore{
						SecretName: routerSecret.Name,
					},
				},
			},
			Locations: []ispnv1.InfinispanSiteLocationSpec{
				{
					Name: "remote-site-1",
					URL:  "infinispan+xsite://fake-site-1.svc.local:7900",
				},
			},
		}
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		podSpec := &ss.Spec.Template.Spec
		container := kubernetes.GetContainer(provision.InfinispanContainer, podSpec)
		_assert := assert.New(t)
		_assert.True(kubernetes.VolumeExists(provision.SiteTransportKeystoreVolumeName, podSpec))
		_assert.True(kubernetes.VolumeMountExists(provision.SiteTruststoreVolumeName, container))
		_assert.True(kubernetes.VolumeExists(provision.SiteTruststoreVolumeName, podSpec))
		_assert.True(kubernetes.VolumeMountExists(provision.SiteTruststoreVolumeName, container))

	}
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

func xsiteTlsSecret(name, namespace, filename string, file []byte) *corev1.Secret {
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"password": tutils.KeystorePassword,
		},
		Data: map[string][]byte{
			filename: file,
		},
	}
	testKube.CreateSecret(secret)
	return secret
}

func genericTestForContainerUpdated(ispn ispnv1.Infinispan, modifier func(*ispnv1.Infinispan), verifier func(*ispnv1.Infinispan, *appsv1.StatefulSet)) {
	testKube.CreateInfinispan(&ispn, tutils.Namespace)
	testKube.WaitForInfinispanPods(int(ispn.Spec.Replicas), tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)

	verifyStatefulSetUpdate(ispn, modifier, verifier)
}

func verifyStatefulSetUpdate(ispn ispnv1.Infinispan, modifier func(*ispnv1.Infinispan), verifier func(*ispnv1.Infinispan, *appsv1.StatefulSet)) {
	// Get the associate StatefulSet
	ss := appsv1.StatefulSet{}
	// Get the current generation
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.GetStatefulSetName()}, &ss))
	generation := ss.Status.ObservedGeneration

	tutils.ExpectNoError(testKube.UpdateInfinispan(&ispn, func() {
		modifier(&ispn)
		// Explicitly call Default to replicate the defaulting webhook
		ispn.Default()
	}))

	// Wait for a new generation to appear
	err := wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, &ss))
		return ss.Status.ObservedGeneration >= generation+1, nil
	})
	tutils.ExpectNoError(err)

	// Wait that current and update revisions match
	// this ensures that the rolling upgrade completes
	err = wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, &ss))
		return ss.Status.CurrentRevision == ss.Status.UpdateRevision, nil
	})
	tutils.ExpectNoError(err)

	// Check that the update has been propagated
	verifier(&ispn, &ss)
}
