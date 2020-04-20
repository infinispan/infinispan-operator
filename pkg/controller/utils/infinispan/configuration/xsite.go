package configuration

import (
	"context"
	"fmt"
	"net/url"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	restclient "k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (xsite XSite) ComputeXSite(infinispan *ispnv1.Infinispan, kubernetes *util.Kubernetes, scheme *runtime.Scheme, logger logr.Logger) error {
	if infinispan.HasSites() {
		siteServiceName := infinispan.GetSiteServiceName()
		siteService, err := GetOrCreateSiteService(siteServiceName, infinispan, kubernetes.Client, scheme, logger)
		if err != nil {
			logger.Error(err, "could not get or create site service")
			return err
		}

		localSiteHost, localSitePort, err := getCrossSiteServiceHostPort(siteService, kubernetes, logger)
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

func ApplyLabelsToCoordinatorsPod(podList *corev1.PodList, infinispan *ispnv1.Infinispan, cluster util.ClusterInterface, client client.Client, logger logr.Logger) bool {
	pass, err := cluster.GetPassword(consts.DefaultOperatorUser, infinispan.GetSecretName(), infinispan.GetNamespace())
	coordinatorFound := false
	if err != nil {
		logger.Error(err, "Error in getting cluster password for x-site")
		return coordinatorFound
	}
	protocol := string(infinispan.GetEndpointScheme())
	for _, item := range podList.Items {
		cacheManagerInfo, err := cluster.GetCacheManagerInfo(consts.DefaultOperatorUser, pass, consts.DefaultCacheManagerName, item.Name, item.Namespace, protocol)
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

func (xsite *XSite) AppendRemoteLocation(infinispan *ispnv1.Infinispan, remoteLocation *ispnv1.InfinispanSiteLocationSpec, kubernetes *util.Kubernetes, logger logr.Logger) error {
	restConfig, err := getRemoteSiteRESTConfig(infinispan, remoteLocation, kubernetes, logger)
	if err != nil {
		return err
	}

	remoteKubernetes, err := util.NewKubernetesFromConfig(restConfig)
	if err != nil {
		logger.Error(err, "could not connect to remote location URL", "URL", remoteLocation.URL)
		return err
	}

	err = xsite.AppendKubernetesRemoteLocation(infinispan, remoteLocation, remoteKubernetes, logger)
	if err != nil {
		return err
	}
	return nil
}

func (xsite *XSite) AppendKubernetesRemoteLocation(infinispan *ispnv1.Infinispan, remoteLocation *ispnv1.InfinispanSiteLocationSpec, remoteKubernetes *util.Kubernetes, logger logr.Logger) error {
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

	backupSite := BackupSite{
		Address: host,
		Name:    remoteLocation.Name,
		Port:    port,
	}

	xsite.Backups = append(xsite.Backups, backupSite)
	return nil
}

func getCrossSiteServiceHostPort(service *corev1.Service, kubernetes *util.Kubernetes, logger logr.Logger) (string, int32, error) {
	switch serviceType := service.Spec.Type; serviceType {
	case corev1.ServiceTypeNodePort:
		// If configuring NodePort, expect external IPs to be configured
		return getNodePortServiceHostPort(service, kubernetes, logger)
	case corev1.ServiceTypeLoadBalancer:
		return getLoadBalancerServiceHostPort(service, logger)
	default:
		return "", 0, fmt.Errorf("unsupported service type '%v'", serviceType)
	}

}

func getNodePortServiceHostPort(service *corev1.Service, k *util.Kubernetes, logger logr.Logger) (string, int32, error) {
	//The IPs must be fetch. Some cases, the API server (which handles REST requests) isn't the same as the worker
	//So, we get the workers list. It needs some permissions cluster-reader permission
	//oc create clusterrolebinding <name> -n ${NAMESPACE} --clusterrole=cluster-reader --serviceaccount=${NAMESPACE}:<account-name>
	workerList := &corev1.NodeList{}

	//select workers only. AFAIK, only the workers have the proxy to the pods
	labelSelector := labels.SelectorFromSet(map[string]string{"node-role.kubernetes.io/worker": ""})

	listOps := &client.ListOptions{
		LabelSelector: labelSelector,
	}
	err := k.Client.List(context.TODO(), workerList, listOps)
	if err != nil {
		return "", 0, err
	}

	for _, node := range workerList.Items {
		//host := k.PublicIP() //returns REST API endpoint. not good.
		//iterate over the all the nodes and return the first ready.
		nodeStatus := node.Status
		for _, nodeCondition := range nodeStatus.Conditions {
			if nodeCondition.Type == corev1.NodeReady && nodeCondition.Status == corev1.ConditionTrue && len(nodeStatus.Addresses) > 0 {
				//The port can be found in the service description
				host := nodeStatus.Addresses[0].Address
				port := service.Spec.Ports[0].NodePort
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
			ip, err := common.LookupHost(ingress.Hostname, logger)

			// Load balancer gets created asynchronously,
			// so it might take time for the status to be updated.
			return ip, port, err
		}
	}

	return "", port, nil
}

func getRemoteSiteRESTConfig(infinispan *ispnv1.Infinispan, location *ispnv1.InfinispanSiteLocationSpec, kubernetes *util.Kubernetes, logger logr.Logger) (*restclient.Config, error) {
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
		return kubernetes.GetMinikubeRESTConfig(copyURL.String(), location.SecretName, infinispan, logger)
	case "openshift":
		return kubernetes.GetOpenShiftRESTConfig(copyURL.String(), location.SecretName, infinispan, logger)
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
