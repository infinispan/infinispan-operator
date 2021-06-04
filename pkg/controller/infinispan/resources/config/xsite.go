package config

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	config "github.com/infinispan/infinispan-operator/pkg/infinispan/configuration"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider/service/helpers"
)

const (
	SchemeTypeKubernetes = "kubernetes"
	SchemeTypeMinikube   = "minikube"
	SchemeTypeOpenShift  = "openshift"

	XSiteLocalServiceTypeUnsupported  = "XSiteLocalServiceUnsupported"
	XSiteRemoteServiceTypeUnsupported = "XSiteRemoteServiceUnsupported"
)

// ComputeXSite compute the xsite struct for cross site function
func ComputeXSite(infinispan *ispnv1.Infinispan, kubernetes *kube.Kubernetes, service *corev1.Service, logger logr.Logger, eventRec record.EventRecorder) (*config.XSite, error) {
	siteServiceName := infinispan.GetSiteServiceName()
	localSiteHost, localSitePort, err := getCrossSiteServiceHostPort(service, kubernetes, logger, eventRec, XSiteLocalServiceTypeUnsupported)
	if err != nil {
		logger.Error(err, "error retrieving local x-site service information")
		return nil, err
	}

	if localSiteHost == "" {
		msg := "local x-site service host not yet available"
		logger.Info(msg)
		return nil, fmt.Errorf(msg)
	}

	logger.Info("local site service", "service name", siteServiceName, "host", localSiteHost, "port", localSitePort)

	xsite := &config.XSite{
		Address: localSiteHost,
		Name:    infinispan.Spec.Service.Sites.Local.Name,
		Port:    localSitePort,
	}

	for _, remoteLocation := range infinispan.GetRemoteSiteLocations() {
		backupSiteURL, err := url.Parse(remoteLocation.URL)
		if err != nil {
			return nil, err
		}
		if backupSiteURL.Scheme == "" || (backupSiteURL.Scheme == consts.StaticCrossSiteUriSchema && backupSiteURL.Hostname() == "") {
			// No static location provided. Try to resolve internal cluster service
			if infinispan.GetRemoteSiteClusterName(remoteLocation.Name) == infinispan.Name && infinispan.GetRemoteSiteNamespace(remoteLocation.Name) == infinispan.Namespace {
				return nil, fmt.Errorf("unable to link the cross-site service with itself. clusterName '%s' or namespace '%s' for remote location '%s' should be different from the original cluster name or namespace",
					infinispan.GetRemoteSiteClusterName(remoteLocation.Name), infinispan.GetRemoteSiteNamespace(remoteLocation.Name), remoteLocation.Name)
			}
			// Add cross-site FQN service name inside the same k8s cluster
			appendBackupSite(remoteLocation.Name, infinispan.GetRemoteSiteServiceFQN(remoteLocation.Name), 0, xsite)
		} else if backupSiteURL.Scheme == consts.StaticCrossSiteUriSchema {
			port, _ := strconv.ParseInt(backupSiteURL.Port(), 10, 32)
			appendBackupSite(remoteLocation.Name, backupSiteURL.Hostname(), int32(port), xsite)
		} else {
			// lookup remote service via kubernetes API
			if err = appendRemoteLocation(infinispan, &remoteLocation, kubernetes, logger, eventRec, xsite); err != nil {
				return nil, err
			}
		}
	}

	logger.Info("x-site configured", "configuration", xsite)
	return xsite, nil
}

func appendRemoteLocation(infinispan *ispnv1.Infinispan, remoteLocation *ispnv1.InfinispanSiteLocationSpec, kubernetes *kube.Kubernetes,
	logger logr.Logger, eventRec record.EventRecorder, xsite *config.XSite) error {
	restConfig, err := getRemoteSiteRESTConfig(infinispan.Namespace, remoteLocation, kubernetes, logger)
	if err != nil {
		return err
	}

	remoteKubernetes, err := kube.NewKubernetesFromConfig(restConfig)
	if err != nil {
		logger.Error(err, "could not connect to remote location URL", "URL", remoteLocation.URL)
		return err
	}

	if err = appendKubernetesRemoteLocation(infinispan, remoteLocation.Name, remoteKubernetes, logger, eventRec, xsite); err != nil {
		return err
	}
	return nil
}

func appendKubernetesRemoteLocation(infinispan *ispnv1.Infinispan, remoteLocationName string, remoteKubernetes *kube.Kubernetes, logger logr.Logger, eventRec record.EventRecorder, xsite *config.XSite) error {
	remoteNamespace := infinispan.GetRemoteSiteNamespace(remoteLocationName)
	remoteServiceName := infinispan.GetRemoteSiteServiceName(remoteLocationName)

	siteService := &corev1.Service{}
	err := remoteKubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: remoteServiceName, Namespace: remoteNamespace}, siteService)
	if err != nil {
		logger.Error(err, "could not get x-site service in remote cluster", "site service name", remoteServiceName, "site namespace", remoteNamespace)
		return err
	}

	host, port, err := getCrossSiteServiceHostPort(siteService, remoteKubernetes, logger, eventRec, XSiteRemoteServiceTypeUnsupported)
	if err != nil {
		logger.Error(err, "error retrieving remote x-site service information")
		return err
	}

	if host == "" {
		msg := "remote x-site service host not yet available"
		logger.Info(msg)
		return fmt.Errorf(msg)
	}

	logger.Info("remote site service", "service name", remoteServiceName, "host", host, "port", port)
	appendBackupSite(remoteLocationName, host, port, xsite)
	return nil
}

func appendBackupSite(name, host string, port int32, xsite *config.XSite) {
	if port == 0 {
		port = consts.CrossSitePort
	}

	backupSite := config.BackupSite{
		Address: host,
		Name:    name,
		Port:    port,
	}

	xsite.Backups = append(xsite.Backups, backupSite)
}

func getCrossSiteServiceHostPort(service *corev1.Service, kubernetes *kube.Kubernetes, logger logr.Logger, eventRec record.EventRecorder, reason string) (string, int32, error) {
	switch serviceType := service.Spec.Type; serviceType {
	case corev1.ServiceTypeNodePort:
		// If configuring NodePort, expect external IPs to be configured
		nodePort := service.Spec.Ports[0].NodePort
		nodeHost, err := kubernetes.GetNodeHost(logger)
		return nodeHost, nodePort, err
	case corev1.ServiceTypeLoadBalancer:
		return getLoadBalancerServiceHostPort(service, logger, eventRec, reason)
	case corev1.ServiceTypeClusterIP:
		return service.Name, service.Spec.Ports[0].Port, nil
	default:
		return "", 0, fmt.Errorf("unsupported service type '%v'", serviceType)
	}
}

func getLoadBalancerServiceHostPort(service *corev1.Service, logger logr.Logger, eventRec record.EventRecorder, reason string) (string, int32, error) {
	port := service.Spec.Ports[0].Port

	// If configuring load balancer, look for external ingress
	if len(service.Status.LoadBalancer.Ingress) > 0 {
		ingress := service.Status.LoadBalancer.Ingress[0]
		if ingress.IP != "" {
			return ingress.IP, port, nil
		}
		if ingress.Hostname != "" {
			// Resolve load balancer host
			ip, err := lookupHost(ingress.Hostname, logger)

			// Load balancer gets created asynchronously,
			// so it might take time for the status to be updated.
			return ip, port, err
		}
	}
	if !helpers.HasLBFinalizer(service) {
		errMsg := "LoadBalancer expose type is not supported on the target platform for x-site"
		if eventRec != nil {
			eventRec.Event(service, corev1.EventTypeWarning, reason, errMsg)
		}
		return "", port, fmt.Errorf(errMsg)
	}

	return "", port, nil
}

func getRemoteSiteRESTConfig(namespace string, location *ispnv1.InfinispanSiteLocationSpec, kubernetes *kube.Kubernetes, logger logr.Logger) (*restclient.Config, error) {
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
	case SchemeTypeKubernetes, SchemeTypeMinikube:
		return kubernetes.GetKubernetesRESTConfig(copyURL.String(), location.SecretName, namespace, logger)
	case SchemeTypeOpenShift:
		return kubernetes.GetOpenShiftRESTConfig(copyURL.String(), location.SecretName, namespace, logger)
	default:
		return nil, fmt.Errorf("backup site URL scheme '%s' not supported for remote connection", scheme)
	}
}

func lookupHost(host string, logger logr.Logger) (string, error) {
	addresses, err := net.LookupHost(host)
	if err != nil {
		logger.Error(err, "host does not resolve")
		return "", err
	}
	logger.Info("host resolved", "host", host, "addresses", addresses)
	return host, nil
}
