package listener

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/infinispan/infinispan-operator/controllers"
	"github.com/infinispan/infinispan-operator/launcher"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"gopkg.in/cenkalti/backoff.v1"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	"github.com/r3labs/sse/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(v1.AddToScheme(scheme))
	utilruntime.Must(v2alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
}

type Parameters struct {
	// The Name of the Infinispan cluster to listen to
	Cluster string
	// The Namespace of the cluster
	Namespace  string
	ZapOptions *zap.Options
}

func New(ctx context.Context, p Parameters) {
	log := zap.NewRaw(zap.UseFlagOptions(p.ZapOptions)).Sugar()

	log.Info(fmt.Sprintf("Starting Infinispan ConfigListener Version: %s", launcher.Version))

	ctx, cancel := context.WithCancel(ctx)
	k8s, err := kubernetes.NewKubernetesFromConfig(ctrl.GetConfigOrDie(), scheme)
	if err != nil {
		log.Fatal("failed to create client")
	}

	infinispan := &v1.Infinispan{}
	loadInfinispan := func() {
		if err = k8s.Client.Get(ctx, types.NamespacedName{Namespace: p.Namespace, Name: p.Cluster}, infinispan); err != nil {
			log.Fatalf("unable to load Infinispan cluster %s in Namespace %s: %v", p.Cluster, p.Namespace, err)
		}
	}
	loadInfinispan()

	secret := &corev1.Secret{}
	if err = k8s.Client.Get(ctx, types.NamespacedName{Namespace: p.Namespace, Name: infinispan.GetAdminSecretName()}, secret); err != nil {
		log.Fatalf("unable to load Infinispan Admin identities secret %s in Namespace %s: %v", p.Cluster, p.Namespace, err)
	}

	user := secret.Data[constants.AdminUsernameKey]
	password := secret.Data[constants.AdminPasswordKey]
	service := fmt.Sprintf("%s.%s.svc.cluster.local:11223", infinispan.GetAdminServiceName(), p.Namespace)
	serviceWithAuth := fmt.Sprintf("http://%s:%s@%s", user, password, service)

	versionManager, err := version.ManagerFromEnv(v1.OperatorOperandVersionEnvVarName)
	if err != nil {
		log.Fatalf("unable to load Operand versions: %v", err)
	}

	cacheListener := &controllers.CacheListener{
		Infinispan:     infinispan,
		Ctx:            ctx,
		Kubernetes:     k8s,
		Log:            log,
		VersionManager: versionManager,
	}

	wait := func() {
		t := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			t.Stop()
			log.Info("Context cancelled, terminating.")
			os.Exit(0)
		case <-t.C:
		}
	}

	for {
		podList := &corev1.PodList{}
		err = k8s.ResourcesList(infinispan.Namespace, infinispan.PodSelectorLabels(), podList, ctx)
		if err != nil {
			log.Error("Unable to retrieve Infinispan pod list: %w", err)
		}

		var readyPod *corev1.Pod
		for _, pod := range podList.Items {
			if kubernetes.IsPodReady(pod) {
				readyPod = &pod
				break
			}
		}

		if readyPod == nil {
			log.Info("Waiting for an Infinispan pod to become ready...")
		} else if err = cacheListener.RemoveStaleResources(readyPod.Name); err != nil {
			log.Errorf("Unable to remove stale resources: %v", err)
		} else {
			break
		}
		wait()
	}

	log.Infof("Consuming streams from service '%s'\n", service)
	containerSse := sse.NewClient(serviceWithAuth + "/rest/v2/container/config?action=listen&includeCurrentState=true")
	containerSse.Connection.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	containerSse.Headers = map[string]string{
		"Accept": string(mime.ApplicationJson),
	}
	containerSse.ReconnectStrategy = backoff.NewConstantBackOff(time.Second)
	containerSse.ReconnectNotify = func(e error, t time.Duration) {
		log.Warnf("Cache stream connection lost. Reconnecting: %v", e)
		// Reload the Infinispan CR as the StatefulSet may have been updated due to a Hot Rod rolling upgrade
		loadInfinispan()
	}

	go func() {
		err = containerSse.SubscribeRawWithContext(ctx, func(msg *sse.Event) {
			var err error
			event := string(msg.Event)
			log.Debugf("ConfigListener received event '%s':\n---\n%s\n---\n", event, msg.Data)
			switch event {
			case "create-cache", "update-cache":
				err = cacheListener.CreateOrUpdate(msg.Data)
			case "remove-cache":
				err = cacheListener.Delete(msg.Data)
			}
			if err != nil {
				log.Errorf("Error encountered for event '%s': %v", event, err)
			}
		})
		if err != nil {
			log.Errorf("Error encountered on SSE subscribe: %v", err)
			cancel()
		}
	}()
	<-ctx.Done()
}
