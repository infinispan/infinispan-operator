package infinispan

import (
	"context"
	"fmt"
	"net"
	"net/url"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	config "github.com/infinispan/infinispan-operator/pkg/infinispan/configuration"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	restclient "k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ComputeXSite compute the xsite struct for cross site function
func ComputeXSite(infinispan *ispnv1.Infinispan, kubernetes *kube.Kubernetes, service *corev1.Service, logger logr.Logger, xsite *config.XSite) error {
	if infinispan.HasSites() {
		siteServiceName := infinispan.GetSiteServiceName()
		localSiteHost, localSitePort, err := getCrossSiteServiceHostPort(service, kubernetes, logger)
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

		xsite.Address = localSiteHost
		xsite.Name = infinispan.Spec.Service.Sites.Local.Name
		xsite.Port = localSitePort

		remoteLocations := findRemoteLocations(xsite.Name, infinispan)
		for _, remoteLocation := range remoteLocations {
			var err error
			if remoteLocation.Host != "" {
				err = appendBackupSite(&remoteLocation, xsite)
			} else if remoteLocation.URL != "" { // lookup remote service via kubernetes api
				err = appendRemoteLocation(infinispan, &remoteLocation, kubernetes, logger, xsite)
			} else {
				msg := fmt.Sprintf("invalid xsite location, remote name: %s does not define host or url", remoteLocation.Name)
				logger.Info(msg)
				err = fmt.Errorf(msg)
			}
			if err != nil {
				return err
			}
		}

		logger.Info("x-site configured", "configuration", xsite)
	}
	return nil
}

func applyLabelsToCoordinatorsPod(podList *corev1.PodList, cluster ispn.ClusterInterface, client client.Client, logger logr.Logger) bool {
	coordinatorFound := false
	for _, item := range podList.Items {
		cacheManagerInfo, err := cluster.GetCacheManagerInfo(consts.DefaultCacheManagerName, item.Name)
		if err == nil {
			lab, ok := item.Labels["coordinator"]
			if cacheManagerInfo["coordinator"].(bool) {
				if !ok || lab != "true" {
					item.Labels["coordinator"] = "true"
					err = client.Update(context.TODO(), &item)
				}
				coordinatorFound = (err == nil)
			} else {
				if ok && lab == "true" {
					// If present leave the label but false the value
					if ok {
						item.Labels["coordinator"] = "false"
						err = client.Update(context.TODO(), &item)
					}
				}
			}
		}
		if err != nil {
			logger.Error(err, "Generic error in managing x-site coordinators")
		}
	}
	return coordinatorFound
}

func appendRemoteLocation(infinispan *ispnv1.Infinispan, remoteLocation *ispnv1.InfinispanSiteLocationSpec, kubernetes *kube.Kubernetes,
	logger logr.Logger, xsite *config.XSite) error {
	restConfig, err := getRemoteSiteRESTConfig(infinispan, remoteLocation, kubernetes, logger)
	if err != nil {
		return err
	}

	remoteKubernetes, err := kube.NewKubernetesFromConfig(restConfig)
	if err != nil {
		logger.Error(err, "could not connect to remote location URL", "URL", remoteLocation.URL)
		return err
	}

	err = appendKubernetesRemoteLocation(infinispan, remoteLocation, remoteKubernetes, logger, xsite)
	if err != nil {
		return err
	}
	return nil
}

func appendKubernetesRemoteLocation(infinispan *ispnv1.Infinispan, remoteLocation *ispnv1.InfinispanSiteLocationSpec,
	remoteKubernetes *kube.Kubernetes, logger logr.Logger, xsite *config.XSite) error {
	siteServiceName := infinispan.GetSiteServiceName()
	namespacedName := types.NamespacedName{Name: siteServiceName, Namespace: infinispan.Namespace}
	siteService := &corev1.Service{}
	err := remoteKubernetes.Client.Get(context.TODO(), namespacedName, siteService)
	if err != nil {
		logger.Error(err, "could not find x-site service in remote cluster", "site service name", siteServiceName)
		return err
	}

	host, port, err := getCrossSiteServiceHostPort(siteService, remoteKubernetes, logger)
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
	remoteLocation.Host = host
	remoteLocation.Port = port
	return appendBackupSite(remoteLocation, xsite)
}

func appendBackupSite(remoteLocation *ispnv1.InfinispanSiteLocationSpec, xsite *config.XSite) error {

	host := remoteLocation.Host
	port := remoteLocation.Port
	if port == 0 {
		port = consts.CrossSitePort
	}

	backupSite := config.BackupSite{
		Address: host,
		Name:    remoteLocation.Name,
		Port:    port,
	}

	xsite.Backups = append(xsite.Backups, backupSite)
	return nil
}

func getCrossSiteServiceHostPort(service *corev1.Service, kubernetes *kube.Kubernetes, logger logr.Logger) (string, int32, error) {
	switch serviceType := service.Spec.Type; serviceType {
	case corev1.ServiceTypeNodePort:
		// If configuring NodePort, expect external IPs to be configured
		return GetNodePortServiceHostPort(service.Spec.Ports[0].NodePort, kubernetes, logger)
	case corev1.ServiceTypeLoadBalancer:
		return getLoadBalancerServiceHostPort(service, logger)
	default:
		return "", 0, fmt.Errorf("unsupported service type '%v'", serviceType)
	}

}

func GetNodePortServiceHostPort(nodePort int32, k *kube.Kubernetes, logger logr.Logger) (string, int32, error) {
	//The IPs must be fetch. Some cases, the API server (which handles REST requests) isn't the same as the worker
	//So, we get the workers list. It needs some permissions cluster-reader permission
	//oc create clusterrolebinding <name> -n ${NAMESPACE} --clusterrole=cluster-reader --serviceaccount=${NAMESPACE}:<account-name>
	workerList := &corev1.NodeList{}

	//select workers first
	req, err := labels.NewRequirement("node-role.kubernetes.io/worker", selection.Exists, nil)
	if err != nil {
		return "", 0, err
	}
	listOps := &client.ListOptions{
		LabelSelector: labels.NewSelector().Add(*req),
	}
	err = k.Client.List(context.TODO(), workerList, listOps)

	if err != nil || len(workerList.Items) == 0 {
		// Fallback selecting everything
		err = k.Client.List(context.TODO(), workerList, &client.ListOptions{})
		if err != nil {
			return "", 0, err
		}
	}

	for _, node := range workerList.Items {
		//host := k.PublicIP() //returns REST API endpoint. not good.
		//iterate over the all the nodes and return the first ready.
		nodeStatus := node.Status
		for _, nodeCondition := range nodeStatus.Conditions {
			if nodeCondition.Type == corev1.NodeReady && nodeCondition.Status == corev1.ConditionTrue && len(nodeStatus.Addresses) > 0 {
				//The port can be found in the service description
				host := nodeStatus.Addresses[0].Address
				port := nodePort
				logger.Info("Found ready worker node.", "Host", host, "Port", port)
				return host, port, nil
			}
		}
	}

	err = fmt.Errorf("no worker node found")
	return "", 0, err
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
			ip, err := lookupHost(ingress.Hostname, logger)

			// Load balancer gets created asynchronously,
			// so it might take time for the status to be updated.
			return ip, port, err
		}
	}

	return "", port, nil
}

func getRemoteSiteRESTConfig(infinispan *ispnv1.Infinispan, location *ispnv1.InfinispanSiteLocationSpec, kubernetes *kube.Kubernetes, logger logr.Logger) (*restclient.Config, error) {
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
	namespace := infinispan.Namespace

	switch scheme := backupSiteURL.Scheme; scheme {
	case "minikube":
		return kubernetes.GetMinikubeRESTConfig(copyURL.String(), location.SecretName, namespace, logger)
	case "openshift":
		return kubernetes.GetOpenShiftRESTConfig(copyURL.String(), location.SecretName, namespace, logger)
	default:
		return nil, fmt.Errorf("backup site URL scheme '%s' not supported", scheme)
	}
}

func findRemoteLocations(localSiteName string, infinispan *ispnv1.Infinispan) (remoteLocations []ispnv1.InfinispanSiteLocationSpec) {
	locations := infinispan.Spec.Service.Sites.Locations
	for _, location := range locations {
		if localSiteName != location.Name {
			remoteLocations = append(remoteLocations, location)
		}
	}

	return
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
