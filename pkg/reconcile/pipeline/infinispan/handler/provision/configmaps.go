package provision

import (
	"bytes"
	"fmt"

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
		extras := CalculateExtraParameters(i, ctx)
		PopulateServerConfigMap(config.ServerBaseConfig, config.ServerAdminConfig, config.ZeroConfig, config.Log4j, extras, configmap)
		configmap.Labels = i.Labels("infinispan-configmap-configuration")
		return nil
	}
	_, _ = ctx.Resources().CreateOrUpdate(configmap, true, mutateFn, pipeline.RetryOnErr)
}

func PopulateServerConfigMap(baseConfig, adminConfig, zeroConfig, log4jConfig string, extras map[string]string, cm *corev1.ConfigMap) {
	cm.Data = map[string]string{
		"infinispan-admin.xml": adminConfig,
		"infinispan-base.xml":  baseConfig,
		"infinispan-zero.xml":  zeroConfig,
		"log4j.xml":            log4jConfig,
	}

	if extras != nil {
		for k, v := range extras {
			cm.Data[k] = v
		}
	}
}

func CalculateExtraParameters(i *ispnv1.Infinispan, ctx pipeline.Context) map[string]string {
	extras := make(map[string]string)
	extras["overrides.env"] = CalculateOverridesEnv(i, ctx)
	return extras
}

func CalculateOverridesEnv(infinispan *ispnv1.Infinispan, ctx pipeline.Context) string {
	var buffer bytes.Buffer
	if infinispan.Spec.Overrides != nil && len(infinispan.Spec.Overrides.Targets) > 0 {
		ctx.Log().Info("Overriding pod parameters", "params", infinispan.Spec.Overrides.Targets)
		podList, err := ctx.InfinispanPods()
		if err != nil {
			return ""
		}

		for _, target := range infinispan.Spec.Overrides.Targets {
			if target.Offset < 0 || len(podList.Items) <= target.Offset {
				continue
			}
			pod := &podList.Items[target.Offset]

			ctx.Log().Info("Updating pod now", "pod", pod.Name, "safe-mode", target.SafeMode)

			args := ""
			if target.SafeMode {
				args += "--safe-mode "
			}

			if args != "" {
				buffer.WriteString(fmt.Sprintf("%s=%s\n", pod.Name, args))
			}
		}
	}
	// Writing an empty string does not delete the file and does not remove the file contents.
	// Keep the new line so we override any existing content with only the new line.
	buffer.WriteString("\n")
	return buffer.String()
}
