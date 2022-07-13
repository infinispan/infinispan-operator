package configure

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	restclient "k8s.io/client-go/rest"
	"k8s.io/cloud-provider/service/helpers"
)

func XSite(i *ispnv1.Infinispan, ctx pipeline.Context) {
	svc := &corev1.Service{}
	if err := ctx.Resources().Load(i.GetSiteServiceName(), svc, pipeline.RetryOnErr); err != nil {
		return
	}
	xSite := &pipeline.XSite{}

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

	appendBackupSite(i.Spec.Service.Sites.Local.Name, svc.Name, localPort, xSite)

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
	ctx.Log().Info("x-site configured", "configuration", xSite)
}

func searchRemoteSites(i *ispnv1.Infinispan, ctx pipeline.Context, xSite *pipeline.XSite) error {
	for _, remoteLocation := range i.GetRemoteSiteLocations() {
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
				return fmt.Errorf("unable to link the cross-site service with itself. clusterName '%s' or namespace '%s' for remote location '%s' should be different from the original cluster name or namespace",
					clusterName, namespace, remoteName)
			}
			// Add cross-site FQN service name inside the same k8s cluster
			appendBackupSite(remoteName, i.GetRemoteSiteServiceFQN(remoteName), 0, xSite)
		} else if backupSiteURL.Scheme == consts.StaticCrossSiteUriSchema {
			port, _ := strconv.ParseInt(backupSiteURL.Port(), 10, 32)
			appendBackupSite(remoteName, backupSiteURL.Hostname(), int32(port), xSite)
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
		ctx.Log().Error(err, "could not connect to remote location URL", "URL", remoteLocation.URL)
		return err
	}

	remoteLocationName := remoteLocation.Name
	remoteNamespace := i.GetRemoteSiteNamespace(remoteLocationName)
	remoteClusterName := i.GetRemoteSiteClusterName(remoteLocationName)

	remoteIspn := &ispnv1.Infinispan{}
	if err := remoteKubernetes.Client.Get(ctx.Ctx(), types.NamespacedName{Name: remoteClusterName, Namespace: remoteNamespace}, remoteIspn); err != nil {
		logger.Error(err, "could not get Infinispan CR from remote cluster", "name", remoteClusterName, "namespace", remoteNamespace, "site", remoteLocationName)
		return err
	}

	if remoteIspn.Spec.Service.Sites.Local.Expose.Type == ispnv1.CrossSiteExposeTypeRoute {
		remoteRouteName := i.GetRemoteSiteRouteName(remoteLocationName, strings.TrimSpace(remoteIspn.Spec.Service.Sites.Local.Expose.RouteHostName) != "")
		// Note: we need to lookup the Route first because, even if Route is enabled, the service exists with "ClusterIP".
		logger.Info("Lookup cross-site route", "Name", remoteRouteName, "Namespace", remoteNamespace)
		siteRoute := &routev1.Route{}
		if err := remoteKubernetes.Client.Get(ctx.Ctx(), types.NamespacedName{Name: remoteRouteName, Namespace: remoteNamespace}, siteRoute); err != nil {
			logger.Error(err, "could not get x-site Route in remote cluster", "site route name", remoteRouteName, "site namespace", remoteNamespace)
			return err
		}
		logger.Info("Remote route found!", "host", siteRoute.Spec.Host)
		appendBackupSite(remoteLocationName, siteRoute.Spec.Host, 443, xSite)
		return nil
	}

	remoteServiceName := i.GetRemoteSiteServiceName(remoteLocationName)
	// No Route object found, try the Service
	logger.Info("Lookup cross-site service", "Name", remoteServiceName, "Namespace", remoteNamespace)
	siteService := &corev1.Service{}
	err = remoteKubernetes.Client.Get(ctx.Ctx(), types.NamespacedName{Name: remoteServiceName, Namespace: remoteNamespace}, siteService)
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

	host, port, err := getCrossSiteServiceHostPort(siteService, ctx, remoteKubernetes, "XSiteRemoteServiceUnsupported")
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
	appendBackupSite(remoteLocationName, host, port, xSite)

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

func appendBackupSite(name, host string, port int32, xSite *pipeline.XSite) {
	if port == 0 {
		port = consts.CrossSitePort
	}

	backupSite := pipeline.BackupSite{
		Address: host,
		Name:    name,
		Port:    port,
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
			return "", port, fmt.Errorf(errMsg)
		}
		return "", port, nil
	case corev1.ServiceTypeClusterIP:
		return service.Name, consts.CrossSitePort, nil
	default:
		return "", 0, fmt.Errorf("unsupported service type '%v'", serviceType)
	}
}

func TransportTLS(i *ispnv1.Infinispan, ctx pipeline.Context) {
	keyStoreSecret := &corev1.Secret{}
	if err := ctx.Resources().Load(i.GetSiteTransportSecretName(), keyStoreSecret, pipeline.RetryOnErr); err != nil {
		return
	}

	keyStoreFileName := i.GetSiteTransportKeyStoreFileName()
	password := string(keyStoreSecret.Data["password"])
	alias := i.GetSiteTransportKeyStoreAlias()

	if err := validaXSiteTLSKeyStore(keyStoreSecret.Name, keyStoreFileName, password, alias); err != nil {
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

	ctx.Log().Info("Transport TLS Configured.", "Keystore", keyStoreFileName, "Secret Name", keyStoreSecret.Name)

	trustStoreSecret := &corev1.Secret{}
	// Only configure Truststore if the Secret exists
	if err := ctx.Resources().Load(i.GetSiteTrustoreSecretName(), trustStoreSecret); err != nil {
		if !errors.IsNotFound(err) {
			ctx.Requeue(err)
		}
		return
	}
	trustStoreFileName := i.GetSiteTrustStoreFileName()
	password = string(trustStoreSecret.Data["password"])

	if err := validaXSiteTLSTrustStore(trustStoreSecret.Name, trustStoreFileName, password); err != nil {
		ctx.Stop(err)
		return
	}
	ctx.Log().Info("Found Truststore.", "Truststore", trustStoreFileName, "Secret Name", trustStoreSecret.ObjectMeta.Name)
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

	if err := validaXSiteTLSKeyStore(keyStoreSecret.Name, filename, password, alias); err != nil {
		ctx.Stop(err)
		return
	}

	log := ctx.Log().WithName("GossipRouter")
	log.Info("TLS Configured.", "Keystore", filename, "Secret Name", keyStoreSecret.Name)

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
		log.Info("No TrustStore secret found")
	}
}

func validaXSiteTLSKeyStore(secretName, filename, password, alias string) error {
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

func validaXSiteTLSTrustStore(secretName, filename, password string) error {
	if len(filename) == 0 {
		return fmt.Errorf("filename is required for KeyStore stored in Secret %s", secretName)
	}
	if len(password) == 0 {
		return fmt.Errorf("password is required for Keystore stored in Secret %s", secretName)
	}
	return nil
}
