package configuration

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime"
	"net/url"

	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	restclient "k8s.io/client-go/rest"
)

// CreateInfinispanConfiguration generates a server configuration
func (xsite *XSite) CreateInfinispanConfiguration(name string, loggingCategories map[string]string, namespace string) InfinispanConfiguration {
	query := fmt.Sprintf("%s-ping.%s.svc.cluster.local", name, namespace)
	jgroups := JGroups{Transport: "tcp", DNSPing: DNSPing{Query: query}}

	config := InfinispanConfiguration{
		ClusterName: name,
		JGroups:     jgroups,
	}

	if len(loggingCategories) > 0 {
		config.Logging = Logging{
			Categories: loggingCategories,
		}
	}

	if xsite != nil {
		config.XSite = *xsite
	}

	return config
}

func (xsite *XSite) AppendRemoteLocation(infinispan *infinispanv1.Infinispan, remoteLocation *infinispanv1.InfinispanSiteLocationSpec, kubernetes *k8s.Kubernetes, logger logr.Logger) error {
	restConfig, err := getRemoteSiteRESTConfig(infinispan, remoteLocation, kubernetes, logger)
	if err != nil {
		return err
	}

	remoteKubernetes, err := k8s.NewKubernetesFromConfig(restConfig)
	if err != nil {
		logger.Error(err, "could not connect to remote location URL", "URL", remoteLocation.URL)
		return err
	}

	err = xsite.appendKubernetesRemoteLocation(infinispan, remoteLocation, remoteKubernetes, logger)
	if err != nil {
		return err
	}
	return nil
}

func (xsite *XSite) appendKubernetesRemoteLocation(infinispan *infinispanv1.Infinispan, remoteLocation *infinispanv1.InfinispanSiteLocationSpec, remoteKubernetes *k8s.Kubernetes, logger logr.Logger) error {
	siteServiceName := infinispan.GetSiteServiceName()
	namespacedName := types.NamespacedName{Name: siteServiceName, Namespace: infinispan.Namespace}
	siteService := &corev1.Service{}
	err := remoteKubernetes.Client.Get(context.TODO(), namespacedName, siteService)
	if err != nil {
		logger.Error(err, "could not find x-site service in remote cluster", "site service name", siteServiceName)
		return err
	}

	host, port, err := getRemoteSiteServiceHostPort(siteService, remoteKubernetes, logger)
	if err != nil {
		logger.Error(err, "error retrieving remote x-site service information")
		return err
	}

	if host == "" {
		msg := "remote x-site service host not yet available"
		logger.Info(msg)
		return fmt.Errorf(msg)
	}

	logger.Info("remote site service",
		"service name", siteServiceName,
		"host", host,
		"port", port,
	)

	backupSite := BackupSite{
		Address: host,
		Name:    remoteLocation.Name,
		Port:    port,
	}

	xsite.Backups = append(xsite.Backups, backupSite)
	return nil
}

func (xsite XSite) ComputeXSite(infinispan *infinispanv1.Infinispan, kubernetes *k8s.Kubernetes, scheme *runtime.Scheme, logger logr.Logger) error {
	if infinispan.HasSites() {
		siteServiceName := infinispan.GetSiteServiceName()
		siteService, err := GetOrCreateSiteService(siteServiceName, infinispan, kubernetes.Client, scheme, logger)
		if err != nil {
			logger.Error(err, "could not get or create site service")
			return err
		}

		localSiteHost, localSitePort, err := getLocalSiteServiceHostPort(siteService, infinispan, kubernetes, logger)
		if err != nil {
			logger.Error(err, "error retrieving local x-site service information")
			return err
		}

		if localSiteHost == "" {
			msg := "local x-site service host not yet available"
			logger.Info(msg)
			return fmt.Errorf(msg)
		}

		logger.Info("local site service",
			"service name", siteServiceName,
			"host", localSiteHost,
			"port", localSitePort,
		)

		xsite := &XSite{
			Address: localSiteHost,
			Name:    infinispan.Spec.Service.Sites.Local.Name,
			Port:    localSitePort,
		}

		remoteLocations := findRemoteLocations(xsite.Name, infinispan)
		for _, remoteLocation := range remoteLocations {
			err := xsite.AppendRemoteLocation(infinispan, &remoteLocation, kubernetes, logger)
			if err != nil {
				return err
			}
		}

		logger.Info("x-site configured", "configuration", *xsite)
	}
	return nil
}

func getRemoteSiteServiceHostPort(service *corev1.Service, remoteKubernetes *k8s.Kubernetes, logger logr.Logger) (string, int32, error) {
	switch serviceType := service.Spec.Type; serviceType {
	case corev1.ServiceTypeNodePort:
		// If configuring NodePort, expect external IPs to be configured
		return remoteKubernetes.PublicIP(), remoteKubernetes.GetNodePort(service), nil
	case corev1.ServiceTypeLoadBalancer:
		return getLoadBalancerServiceHostPort(service, logger)
	default:
		return "", 0, fmt.Errorf("unsupported service type '%v'", serviceType)
	}
}

func getRemoteSiteRESTConfig(infinispan *infinispanv1.Infinispan, location *infinispanv1.InfinispanSiteLocationSpec, kubernetes *k8s.Kubernetes, logger logr.Logger) (*restclient.Config, error) {
	backupSiteURL, err := url.Parse(location.URL)
	if err != nil {
		return nil, err
	}

	// Copy URL to make modify it for backup access
	copyURL, err := url.Parse(backupSiteURL.String())
	if err != nil {
		return nil, err
	}

	// All remote sites locations are accessed via encrypted http
	copyURL.Scheme = "https"

	switch scheme := backupSiteURL.Scheme; scheme {
	case "minikube":
		return kubernetes.GetMinikubeRESTConfig(copyURL.String(), location.SecretName, infinispan.Namespace, logger)
	case "openshift":
		return kubernetes.GetOpenShiftRESTConfig(copyURL.String(), location.SecretName, infinispan.Namespace, logger)
	default:
		return nil, fmt.Errorf("backup site URL scheme '%s' not supported", scheme)
	}
}

func getLocalSiteServiceHostPort(service *corev1.Service, infinispan *infinispanv1.Infinispan, remoteKubernetes *k8s.Kubernetes, logger logr.Logger) (string, int32, error) {
	switch serviceType := service.Spec.Type; serviceType {
	case corev1.ServiceTypeNodePort:
		externalIP, err := infinispan.FindNodePortExternalIP()
		if err != nil {
			return "", 0, err
		}

		// If configuring NodePort, expect external IPs to be configured
		return externalIP, remoteKubernetes.GetNodePort(service), nil
	case corev1.ServiceTypeLoadBalancer:
		return getLoadBalancerServiceHostPort(service, logger)
	default:
		return "", 0, fmt.Errorf("unsupported service type '%v'", serviceType)
	}
}

func getLoadBalancerServiceHostPort(service *corev1.Service, logger logr.Logger) (string, int32, error) {
	port := service.Spec.Ports[0].Port

	// If configuring load balancer, look for external ingress
	if len(service.Status.LoadBalancer.Ingress) > 0 {
		ingress := service.Status.LoadBalancer.Ingress[0]
		if ingress.IP != "" {
			return ingress.IP, port, nil
		}
		if ingress.Hostname != "" {
			// Resolve load balancer host
			ip, err := common.LookupHost(ingress.Hostname, logger)

			// Load balancer gets created asynchronously,
			// so it might take time for the status to be updated.
			return ip, port, err
		}
	}

	return "", port, nil
}

func findRemoteLocations(localSiteName string, infinispan *infinispanv1.Infinispan) (remoteLocations []infinispanv1.InfinispanSiteLocationSpec) {
	locations := infinispan.Spec.Service.Sites.Locations
	for _, location := range locations {
		if localSiteName != location.Name {
			remoteLocations = append(remoteLocations, location)
		}
	}

	return
}
