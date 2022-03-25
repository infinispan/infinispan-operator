package listener

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/infinispan/infinispan-operator/launcher"
	"net/http"
	"time"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	"github.com/r3labs/sse/v2"
	"gopkg.in/cenkalti/backoff.v1"
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
	kubernetes, err := kubernetes.NewKubernetesFromConfig(ctrl.GetConfigOrDie(), scheme)
	if err != nil {
		log.Fatal("failed to create client")
	}

	infinispan := &v1.Infinispan{}
	if err = kubernetes.Client.Get(ctx, types.NamespacedName{Namespace: p.Namespace, Name: p.Cluster}, infinispan); err != nil {
		log.Fatalf("unable to load Infinispan cluster %s in Namespace %s: %w", p.Cluster, p.Namespace, err)
	}

	secret := &corev1.Secret{}
	if err = kubernetes.Client.Get(ctx, types.NamespacedName{Namespace: p.Namespace, Name: infinispan.GetAdminSecretName()}, secret); err != nil {
		log.Fatalf("unable to load Infinispan Admin identities secret %s in Namespace %s: %w", p.Cluster, p.Namespace, err)
	}

	service := fmt.Sprintf("%s.%s.svc.cluster.local:11223", infinispan.GetAdminServiceName(), p.Namespace)
	log.Debugf("Attempting to consume streams from service '%s'\n", service)

	user := secret.Data[constants.AdminUsernameKey]
	password := secret.Data[constants.AdminPasswordKey]
	service = fmt.Sprintf("http://%s:%s@%s", user, password, service)

	containerSse := sse.NewClient(service + "/rest/v2/container/config?action=listen&includeCurrentState=true")
	containerSse.Connection.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	containerSse.Headers = map[string]string{
		"Accept": string(mime.ApplicationYaml),
	}
	// TODO how to make this more rebust?
	// Fix number of attempts to reconnect?
	// How should Listener behave if Cluster becomes unavailable at runtime?
	containerSse.ReconnectStrategy = &backoff.StopBackOff{}
	containerSse.ReconnectNotify = func(e error, t time.Duration) {
		log.Warnf("Cache stream connection lost. Reconnecting: %w", e)
	}

	cacheListener := &controllers.CacheListener{
		Infinispan: infinispan,
		Ctx:        ctx,
		Kubernetes: kubernetes,
		Log:        log,
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
				log.Errorf("Error encountered for event '%s': %w", event, err)
			}
		})
		if err != nil {
			log.Errorf("Error encountered on SSE subscribe: %w", err)
			cancel()
		}
	}()
	<-ctx.Done()
}
