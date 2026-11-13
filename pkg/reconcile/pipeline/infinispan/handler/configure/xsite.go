package configure

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	restclient "k8s.io/client-go/rest"
	"k8s.io/cloud-provider/service/helpers"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func XSite(i *ispnv1.Infinispan, ctx pipeline.Context) {
	xSite := &pipeline.XSite{
		HeartbeatEnabled:  *i.Spec.Service.Sites.Local.Discovery.Heartbeats.Enabled,
		HeartbeatInterval: *i.Spec.Service.Sites.Local.Discovery.Heartbeats.Interval,
		HeartbeatTimeout:  *i.Spec.Service.Sites.Local.Discovery.Heartbeats.Timeout,
	}

	if i.IsGossipRouterEnabled() {
		svc := &corev1.Service{}
		if err := ctx.Resources().Load(i.GetSiteServiceName(), svc, pipeline.RetryOnErr); err != nil {
			return
		}

		// Configure Local and Remote sites
		localSiteHost, localPort, err := getCrossSiteServiceHostPort(svc, ctx, ctx.Kubernetes(), "XSiteLocalServiceUnsupported")
		if err != nil {
			ctx.Requeue(fmt.Errorf("error retrieving local x-site service information: %w", err))
			return
		}

		if localSiteHost == "" {
			ctx.Requeue(fmt.Errorf("local x-site service host not yet available"))
			return
		}

		if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
			// For load balancer service, we allow a custom port!
			localPort = consts.CrossSitePort
		}

		appendBackupSite(i.Spec.Service.Sites.Local.Name, svc.Name, localPort, xSite, false)
	} else {
		appendBackupSite(i.Spec.Service.Sites.Local.Name, "", 0, xSite, true)
	}

	if err := searchRemoteSites(i, ctx, xSite); err != nil {
		ctx.Requeue(fmt.Errorf("unable to search remote sites: %w", err))
		return
	}

	// Configure MaxRelayNodes
	if i.Spec.Service.Sites.Local.MaxRelayNodes <= 0 {
		xSite.MaxRelayNodes = 1
	} else {
		xSite.MaxRelayNodes = i.Spec.Service.Sites.Local.MaxRelayNodes
	}
	ctx.ConfigFiles().XSite = xSite
	ctx.Log().V(2).Info("x-site configured", "configuration", xSite)
}

func searchRemoteSites(i *ispnv1.Infinispan, ctx pipeline.Context, xSite *pipeline.XSite) error {
	remoteLocations := i.GetRemoteSiteLocations()
	siteNames := make([]string, len(remoteLocations))
	idx := 0
	for k := range remoteLocations {
		siteNames[idx] = k
		idx++
	}
	sort.Strings(siteNames)
	for _, siteName := range siteNames {
		remoteLocation := remoteLocations[siteName]
		remoteName := remoteLocation.Name
		backupSiteURL, err := url.Parse(remoteLocation.URL)
		if err != nil {
			return err
		}
		if backupSiteURL.Scheme == "" || (backupSiteURL.Scheme == consts.StaticCrossSiteUriSchema && backupSiteURL.Hostname() == "") {
			// No static location provided. Try to resolve internal cluster service
			clusterName := i.GetRemoteSiteClusterName(remoteName)
			namespace := i.GetRemoteSiteNamespace(remoteName)
			if clusterName == i.Name && namespace == i.Namespace {
				appendBackupSite(remoteName, "", 0, xSite, true)
				continue
			}
			// Add cross-site FQN service name inside the same k8s cluster
			appendBackupSite(remoteName, i.GetRemoteSiteServiceFQN(remoteName), 0, xSite, false)
		} else if backupSiteURL.Scheme == consts.StaticCrossSiteUriSchema {
			port, _ := strconv.ParseInt(backupSiteURL.Port(), 10, 32)
			appendBackupSite(remoteName, backupSiteURL.Hostname(), int32(port), xSite, false)
		} else {
			// lookup remote service via kubernetes API
			if err = appendRemoteLocation(i, ctx, xSite, &remoteLocation); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendRemoteLocation(i *ispnv1.Infinispan, ctx pipeline.Context, xSite *pipeline.XSite, remoteLocation *ispnv1.InfinispanSiteLocationSpec) error {
	logger := ctx.Log()
	restConfig, err := getRemoteSiteRESTConfig(i, ctx, remoteLocation)
	if err != nil {
		return err
	}

	remoteKubernetes, err := kube.NewKubernetesFromConfig(restConfig, ctx.Kubernetes().Client.Scheme())
	if err != nil {
		return fmt.Errorf("could not connect to remote site %q: %w", remoteLocation.URL, err)
	}

	remoteLocationName := remoteLocation.Name
	remoteNamespace := i.GetRemoteSiteNamespace(remoteLocationName)
	remoteServiceName := i.GetRemoteSiteServiceName(remoteLocationName)
	remoteRouteName := i.GetRemoteSiteRouteName(remoteLocationName)

	routeSupported, err := remoteKubernetes.IsGroupVersionSupported(pipeline.RouteGVK.GroupVersion().String(), pipeline.RouteGVK.Kind)
	if err != nil {
		return fmt.Errorf("failed to check if Route GVK is supported: %w", err)
	}

	if routeSupported {
		// Note: we need to lookup the Route first because, even if Route is enabled, the service exists with "ClusterIP".
		logger.V(1).Info("Lookup cross-site route", "name", remoteRouteName, "namespace", remoteNamespace)
		siteRoute := &routev1.Route{}
		if err := remoteKubernetes.Client.Get(ctx.Ctx(), types.NamespacedName{Name: remoteRouteName, Namespace: remoteNamespace}, siteRoute); err == nil {
			// Route found
			logger.V(1).Info("Remote route found", "host", siteRoute.Spec.Host)
			appendBackupSite(remoteLocationName, siteRoute.Spec.Host, 443, xSite, false)
			return nil
		} else if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("could not get x-site Route %s/%s in remote cluster: %w", remoteNamespace, remoteRouteName, err)
		}
	}

	// No Route object found, try the Service
	logger.V(1).Info("Lookup cross-site service", "name", remoteServiceName, "namespace", remoteNamespace)
	siteService := &corev1.Service{}
	err = remoteKubernetes.Client.Get(ctx.Ctx(), types.NamespacedName{Name: remoteServiceName, Namespace: remoteNamespace}, siteService)
	if err != nil {
		return fmt.Errorf("could not get x-site service %s/%s in remote cluster: %w", remoteNamespace, remoteServiceName, err)
	}

	if siteService.Spec.Type == corev1.ServiceTypeClusterIP {
		return fmt.Errorf("ClusterIP service type not supported for x-site service %s/%s in remote cluster", remoteNamespace, remoteServiceName)
	}

	host, port, err := getCrossSiteServiceHostPort(siteService, ctx, remoteKubernetes, "XSiteRemoteServiceUnsupported")
	if err != nil {
		return fmt.Errorf("error retrieving remote x-site service information: %w", err)
	}
	if host == "" {
		return fmt.Errorf("remote x-site service %s/%s host not yet available", remoteNamespace, remoteServiceName)
	}

	logger.V(1).Info("Remote site service found", "host", host, "port", port)
	appendBackupSite(remoteLocationName, host, port, xSite, false)

	return nil
}

func getRemoteSiteRESTConfig(i *ispnv1.Infinispan, ctx pipeline.Context, location *ispnv1.InfinispanSiteLocationSpec) (*restclient.Config, error) {
	backupSiteURL, err := url.Parse(location.URL)
	if err != nil {
		return nil, err
	}

	// Copy URL so we can modify it for backup access
	copyURL, err := url.Parse(backupSiteURL.String())
	if err != nil {
		return nil, err
	}

	// All remote sites locations are accessed via encrypted http
	copyURL.Scheme = "https"
	namespace := i.Namespace
	k8s := ctx.Kubernetes()
	switch scheme := backupSiteURL.Scheme; scheme {
	case ispnv1.CrossSiteSchemeTypeKubernetes, ispnv1.CrossSiteSchemeTypeMinikube:
		return k8s.GetKubernetesRESTConfig(copyURL.String(), location.SecretName, namespace, ctx.Log(), ctx.Ctx())
	case ispnv1.CrossSiteSchemeTypeOpenShift:
		return k8s.GetOpenShiftRESTConfig(copyURL.String(), location.SecretName, namespace, ctx.Log(), ctx.Ctx())
	default:
		return nil, fmt.Errorf("backup site URL scheme '%s' not supported for remote connection", scheme)
	}
}

func appendBackupSite(name, host string, port int32, xSite *pipeline.XSite, ignoreGossipRouter bool) {
	if port == 0 {
		port = consts.CrossSitePort
	}

	backupSite := pipeline.BackupSite{
		Address:            host,
		Name:               name,
		Port:               port,
		IgnoreGossipRouter: ignoreGossipRouter,
	}

	xSite.Sites = append(xSite.Sites, backupSite)
}

func getCrossSiteServiceHostPort(service *corev1.Service, ctx pipeline.Context, k8s *kube.Kubernetes, reason string) (string, int32, error) {
	switch serviceType := service.Spec.Type; serviceType {
	case corev1.ServiceTypeNodePort:
		// If configuring NodePort, expect external IPs to be configured
		nodePort := service.Spec.Ports[0].NodePort
		nodeHost, err := k8s.GetNodeHost(ctx.Log(), ctx.Ctx())
		return nodeHost, nodePort, err
	case corev1.ServiceTypeLoadBalancer:
		port := service.Spec.Ports[0].Port
		// If configuring load balancer, look for external ingress
		if len(service.Status.LoadBalancer.Ingress) > 0 {
			ingress := service.Status.LoadBalancer.Ingress[0]
			if ingress.IP != "" {
				return ingress.IP, port, nil
			}
			if ingress.Hostname != "" {
				// Resolve load balancer host
				host := ingress.Hostname
				addresses, err := net.LookupHost(host)
				if err != nil {
					return "", -1, fmt.Errorf("host does not resolve: %w", err)
				}
				ctx.Log().Info("host resolved", "host", host, "addresses", addresses)

				// Load balancer gets created asynchronously,
				// so it might take time for the status to be updated.
				return host, port, err
			}
		}
		if !helpers.HasLBFinalizer(service) {
			errMsg := "LoadBalancer expose type is not supported on the target platform for x-site"
			ctx.EventRecorder().Event(service, corev1.EventTypeWarning, reason, errMsg)
			return "", port, errors.New(errMsg)
		}
		return "", port, nil
	case corev1.ServiceTypeClusterIP:
		return service.Name, consts.CrossSitePort, nil
	default:
		return "", 0, fmt.Errorf("unsupported service type '%v'", serviceType)
	}
}

func TransportTLS(i *ispnv1.Infinispan, ctx pipeline.Context) {
	log := ctx.Log().WithName("xsite")
	keyStoreSecret := &corev1.Secret{}
	if err := ctx.Resources().Load(i.GetSiteTransportSecretName(), keyStoreSecret, pipeline.RetryOnErr); err != nil {
		return
	}

	keyStoreFileName := i.GetSiteTransportKeyStoreFileName()
	password := string(keyStoreSecret.Data["password"])
	alias := i.GetSiteTransportKeyStoreAlias()

	if err := validateXSiteTLSKeyStore(keyStoreSecret.Name, keyStoreFileName, password, alias); err != nil {
		ctx.Stop(err)
		return
	}

	configFiles := ctx.ConfigFiles()
	configFiles.Transport.Keystore = &pipeline.Keystore{
		Alias:    i.GetSiteTransportKeyStoreAlias(),
		Password: string(keyStoreSecret.Data["password"]),
		Path:     fmt.Sprintf("%s/%s", consts.SiteTransportKeyStoreRoot, keyStoreFileName),
		Type:     consts.GetWithDefault(string(keyStoreSecret.Data["type"]), "pkcs12"),
	}

	log.V(1).Info("Transport TLS configured", "keystore", keyStoreFileName, "secret", keyStoreSecret.Name)

	// do not attempt to load the trust store secret if not configured
	if i.GetSiteTrustoreSecretName() == "" {
		log.V(1).Info("Truststore not configured")
		return
	}

	trustStoreSecret := &corev1.Secret{}
	// Only configure Truststore if the Secret exists
	if err := ctx.Resources().Load(i.GetSiteTrustoreSecretName(), trustStoreSecret); err != nil {
		if !apierrors.IsNotFound(err) {
			ctx.Requeue(err)
		}
		return
	}
	trustStoreFileName := i.GetSiteTrustStoreFileName()
	password = string(trustStoreSecret.Data["password"])

	if err := validateXSiteTLSTrustStore(trustStoreSecret.Name, trustStoreFileName, password); err != nil {
		ctx.Stop(err)
		return
	}
	log.V(1).Info("Truststore found", "truststore", trustStoreFileName, "secret", trustStoreSecret.Name)
	configFiles.Transport.Truststore = &pipeline.Truststore{
		File:     trustStoreSecret.Data[trustStoreFileName],
		Path:     fmt.Sprintf("%s/%s", consts.SiteTrustStoreRoot, trustStoreFileName),
		Password: password,
		Type:     consts.GetWithDefault(string(keyStoreSecret.Data["type"]), "pkcs12"),
	}
}

func GossipRouterTLS(i *ispnv1.Infinispan, ctx pipeline.Context) {
	keyStoreSecret := &corev1.Secret{}
	if err := ctx.Resources().Load(i.GetSiteRouterSecretName(), keyStoreSecret, pipeline.RetryOnErr); err != nil {
		return
	}

	filename := i.GetSiteRouterKeyStoreFileName()
	password := string(keyStoreSecret.Data["password"])
	alias := i.GetSiteRouterKeyStoreAlias()

	if err := validateXSiteTLSKeyStore(keyStoreSecret.Name, filename, password, alias); err != nil {
		ctx.Stop(err)
		return
	}

	log := ctx.Log().WithName("gossipRouter")
	log.V(1).Info("TLS configured", "keystore", filename, "secret", keyStoreSecret.Name)

	configFiles := ctx.ConfigFiles()
	gossipRouter := &configFiles.XSite.GossipRouter
	gossipRouter.Keystore = &pipeline.Keystore{
		Alias:    alias,
		Password: password,
		Path:     fmt.Sprintf("%s/%s", consts.SiteRouterKeyStoreRoot, filename),
		Type:     consts.GetWithDefault(string(keyStoreSecret.Data["type"]), "pkcs12"),
	}

	if configFiles.Transport.Truststore != nil {
		// The GossipRouter currently uses the same truststore as the transport, but in the ConfigFiles we differentiate
		// between the two to allow this to change in the future without having to update the provisioning handlers
		gossipRouter.Truststore = configFiles.Transport.Truststore
	} else {
		log.V(1).Info("No truststore secret found")
	}
}

func validateXSiteTLSKeyStore(secretName, filename, password, alias string) error {
	if len(filename) == 0 {
		return fmt.Errorf("filename is required for Keystore stored in Secret %s", secretName)
	}
	if len(password) == 0 {
		return fmt.Errorf("password is required for Keystore stored in Secret %s", secretName)
	}
	if len(alias) == 0 {
		return fmt.Errorf("alias is required for Keystore stored in Secret %s", secretName)
	}
	return nil
}

func validateXSiteTLSTrustStore(secretName, filename, password string) error {
	if len(filename) == 0 {
		return fmt.Errorf("filename is required for KeyStore stored in Secret %s", secretName)
	}
	if len(password) == 0 {
		return fmt.Errorf("password is required for Keystore stored in Secret %s", secretName)
	}
	return nil
}
