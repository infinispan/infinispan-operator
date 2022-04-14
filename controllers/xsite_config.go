package controllers

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	config "github.com/infinispan/infinispan-operator/pkg/infinispan/configuration/server"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider/service/helpers"
)

const (
	SchemeTypeKubernetes = "kubernetes"
	SchemeTypeMinikube   = "minikube"
	SchemeTypeOpenShift  = "openshift"
)

// ComputeXSite compute the xsite struct for cross site function
func ComputeXSite(infinispan *ispnv1.Infinispan, kubernetes *kube.Kubernetes, service *corev1.Service, logger logr.Logger, eventRec record.EventRecorder, ctx context.Context) (*config.XSite, error) {
	siteServiceName := infinispan.GetSiteServiceName()
	// make sure the local service is up and running
	localSiteHost, localPort, err := getCrossSiteServiceHostPort(service, kubernetes, logger, eventRec, "XSiteLocalServiceUnsupported", ctx)
	if err != nil {
		logger.Error(err, "error retrieving local x-site service information")
		return nil, err
	}

	if localSiteHost == "" {
		msg := "local x-site service host not yet available"
		logger.Info(msg)
		return nil, fmt.Errorf(msg)
	}

	if service.Spec.Type != corev1.ServiceTypeLoadBalancer {
		// For load balancer service, we allow a custom port!
		localPort = consts.CrossSitePort
	}

	logger.Info("local site service", "service name", siteServiceName, "host", siteServiceName, "port", localPort)

	maxRelayNodes := infinispan.Spec.Service.Sites.Local.MaxRelayNodes
	if maxRelayNodes <= 0 {
		maxRelayNodes = 1
	}

	// use the local/internal service host & port to avoid unecessary hops with external services
	xsite := &config.XSite{
		MaxRelayNodes: maxRelayNodes,
	}

	// add local site first
	appendBackupSite(infinispan.Spec.Service.Sites.Local.Name, siteServiceName, localPort, xsite)

	err = searchRemoteSites(infinispan, kubernetes, xsite, logger, eventRec, ctx)

	logger.Info("x-site configured", "configuration", xsite)
	return xsite, err
}

func searchRemoteSites(infinispan *ispnv1.Infinispan, kubernetes *kube.Kubernetes, xsite *config.XSite, logger logr.Logger, eventRec record.EventRecorder, ctx context.Context) error {
	for _, remoteLocation := range infinispan.GetRemoteSiteLocations() {
		backupSiteURL, err := url.Parse(remoteLocation.URL)
		if err != nil {
			return err
		}
		if backupSiteURL.Scheme == "" || (backupSiteURL.Scheme == consts.StaticCrossSiteUriSchema && backupSiteURL.Hostname() == "") {
			// No static location provided. Try to resolve internal cluster service
			if infinispan.GetRemoteSiteClusterName(remoteLocation.Name) == infinispan.Name && infinispan.GetRemoteSiteNamespace(remoteLocation.Name) == infinispan.Namespace {
				return fmt.Errorf("unable to link the cross-site service with itself. clusterName '%s' or namespace '%s' for remote location '%s' should be different from the original cluster name or namespace",
					infinispan.GetRemoteSiteClusterName(remoteLocation.Name), infinispan.GetRemoteSiteNamespace(remoteLocation.Name), remoteLocation.Name)
			}
			// Add cross-site FQN service name inside the same k8s cluster
			appendBackupSite(remoteLocation.Name, infinispan.GetRemoteSiteServiceFQN(remoteLocation.Name), 0, xsite)
		} else if backupSiteURL.Scheme == consts.StaticCrossSiteUriSchema {
			port, _ := strconv.ParseInt(backupSiteURL.Port(), 10, 32)
			appendBackupSite(remoteLocation.Name, backupSiteURL.Hostname(), int32(port), xsite)
		} else {
			// lookup remote service via kubernetes API
			if err = appendRemoteLocation(ctx, infinispan, &remoteLocation, kubernetes, logger, eventRec, xsite); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendRemoteLocation(ctx context.Context, infinispan *ispnv1.Infinispan, remoteLocation *ispnv1.InfinispanSiteLocationSpec, kubernetes *kube.Kubernetes,
	logger logr.Logger, eventRec record.EventRecorder, xsite *config.XSite) error {
	restConfig, err := getRemoteSiteRESTConfig(infinispan.Namespace, remoteLocation, kubernetes, logger, ctx)
	if err != nil {
		return err
	}

	remoteKubernetes, err := kube.NewKubernetesFromConfig(restConfig, kubernetes.Client.Scheme())
	if err != nil {
		logger.Error(err, "could not connect to remote location URL", "URL", remoteLocation.URL)
		return err
	}

	if err = appendKubernetesRemoteLocation(ctx, infinispan, remoteLocation.Name, remoteKubernetes, logger, eventRec, xsite); err != nil {
		return err
	}
	return nil
}

func appendKubernetesRemoteLocation(ctx context.Context, infinispan *ispnv1.Infinispan, remoteLocationName string, remoteKubernetes *kube.Kubernetes, logger logr.Logger, eventRec record.EventRecorder, xsite *config.XSite) error {
	remoteNamespace := infinispan.GetRemoteSiteNamespace(remoteLocationName)
	remoteServiceName := infinispan.GetRemoteSiteServiceName(remoteLocationName)
	remoteRouteName := infinispan.GetRemoteSiteRouteName(remoteLocationName)

	routeObj := &reconcileType{
		ObjectType:   &routev1.Route{},
		GroupVersion: routev1.SchemeGroupVersion,
	}
	routeSupported, err := remoteKubernetes.IsGroupVersionSupported(routeObj.GroupVersion.String(), routeObj.Kind())
	if err != nil {
		logger.Error(err, fmt.Sprintf("failed to check if GVK '%s' is supported", routeObj.GroupVersionKind()))
		return err
	}

	if routeSupported {
		// Note: we need to lookup the Route first because, even if Route is enabled, the service exists with "ClusterIP".
		logger.Info("Lookup cross-site route", "Name", remoteRouteName, "Namespace", remoteNamespace)
		siteRoute := &routev1.Route{}
		err = remoteKubernetes.Client.Get(ctx, types.NamespacedName{Name: remoteRouteName, Namespace: remoteNamespace}, siteRoute)
		if err == nil {
			// Route found
			logger.Info("Remote route found!", "host", siteRoute.Spec.Host)
			appendBackupSite(remoteLocationName, siteRoute.Spec.Host, 443, xsite)
			return nil
		}
		if err != nil && !errors.IsNotFound(err) {
			// unexpected error
			logger.Error(err, "could not get x-site Route in remote cluster", "site route name", remoteRouteName, "site namespace", remoteNamespace)
			return err
		}
	}

	// no Route object found, try the Service
	logger.Info("Lookup cross-site service", "Name", remoteServiceName, "Namespace", remoteNamespace)
	siteService := &corev1.Service{}
	err = remoteKubernetes.Client.Get(ctx, types.NamespacedName{Name: remoteServiceName, Namespace: remoteNamespace}, siteService)
	if err != nil {
		logger.Error(err, "could not get x-site service in remote cluster", "site service name", remoteServiceName, "site namespace", remoteNamespace)
		return err
	}

	if siteService.Spec.Type == corev1.ServiceTypeClusterIP {
		// If we reach this point, we have a remote API URL to a different cluster.
		// ClusterIP service won't work because we will be unable to connect to the internal IP address
		err = fmt.Errorf("ClusterIP service type not supported")
		logger.Error(err, "could not get x-site service in remote cluster", "site service name", remoteServiceName, "site namespace", remoteNamespace)
		return err
	}

	host, port, err := getCrossSiteServiceHostPort(siteService, remoteKubernetes, logger, eventRec, "XSiteRemoteServiceUnsupported", ctx)
	if err != nil {
		logger.Error(err, "error retrieving remote x-site service information")
		return err
	}
	if host == "" {
		err = fmt.Errorf("remote x-site service host not yet available")
		logger.Error(err, "could not get x-site service in remote cluster", "site service name", remoteServiceName, "site namespace", remoteNamespace)
		return err
	}

	logger.Info("remote site service", "host", host, "port", port)
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

	xsite.Sites = append(xsite.Sites, backupSite)
}

func getCrossSiteServiceHostPort(service *corev1.Service, kubernetes *kube.Kubernetes, logger logr.Logger, eventRec record.EventRecorder, reason string, ctx context.Context) (string, int32, error) {
	switch serviceType := service.Spec.Type; serviceType {
	case corev1.ServiceTypeNodePort:
		// If configuring NodePort, expect external IPs to be configured
		nodePort := service.Spec.Ports[0].NodePort
		nodeHost, err := kubernetes.GetNodeHost(logger, ctx)
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

func getRemoteSiteRESTConfig(namespace string, location *ispnv1.InfinispanSiteLocationSpec, kubernetes *kube.Kubernetes, logger logr.Logger, ctx context.Context) (*restclient.Config, error) {
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
	case ispnv1.CrossSiteSchemeTypeKubernetes, ispnv1.CrossSiteSchemeTypeMinikube:
		return kubernetes.GetKubernetesRESTConfig(copyURL.String(), location.SecretName, namespace, logger, ctx)
	case ispnv1.CrossSiteSchemeTypeOpenShift:
		return kubernetes.GetOpenShiftRESTConfig(copyURL.String(), location.SecretName, namespace, logger, ctx)
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
