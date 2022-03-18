package provision

import (
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func InfinispanConfigMap(i *ispnv1.Infinispan, ctx pipeline.Context) {
	config := ctx.ConfigFiles()

	configmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      i.GetConfigName(),
			Namespace: i.Namespace,
		},
	}

	mutateFn := func() error {
		PopulateServerConfigMap(config.ServerConfig, config.ZeroConfig, config.Log4j, configmap)
		configmap.Labels = i.Labels("infinispan-configmap-configuration")
		return nil
	}
	_, _ = ctx.Resources().CreateOrUpdate(configmap, true, mutateFn, pipeline.RetryOnErr)
}

func PopulateServerConfigMap(serverConfig, zeroConfig, log4jConfig string, cm *corev1.ConfigMap) {
	cm.Data = map[string]string{
		"infinispan.xml":      serverConfig,
		"infinispan-zero.xml": zeroConfig,
		"log4j.xml":           log4jConfig,
	}
}
