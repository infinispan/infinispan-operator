package config

import (
	"fmt"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/configuration"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const namespace = "testing-namespace"

var staticXSiteInfinispan = &ispnv1.Infinispan{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "example-clustera",
		Namespace: namespace,
	},
	Spec: ispnv1.InfinispanSpec{
		Service: ispnv1.InfinispanServiceSpec{
			Sites: &ispnv1.InfinispanSitesSpec{
				Local: ispnv1.InfinispanSitesLocalSpec{
					Name: "SiteA",
					Expose: ispnv1.CrossSiteExposeSpec{
						Type: ispnv1.CrossSiteExposeTypeClusterIP,
					},
				},
				Locations: []ispnv1.InfinispanSiteLocationSpec{
					{
						Name: "SiteA",
						URL:  "infinispan+xsite://example-clustera-site",
					},
					{
						Name: "SiteB",
						URL:  "infinispan+xsite://example-clusterb-site",
					},
					{
						Name: "SiteC",
						URL:  "infinispan+xsite://example-clusterc-site:7901",
					},
				},
			},
		},
	},
}

var selfStaticXSiteInfinispan = &ispnv1.Infinispan{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "example-clustera",
		Namespace: namespace,
	},
	Spec: ispnv1.InfinispanSpec{
		Service: ispnv1.InfinispanServiceSpec{
			Sites: &ispnv1.InfinispanSitesSpec{
				Local: ispnv1.InfinispanSitesLocalSpec{
					Name: "SiteA",
					Expose: ispnv1.CrossSiteExposeSpec{
						Type: ispnv1.CrossSiteExposeTypeClusterIP,
					},
				},
				Locations: []ispnv1.InfinispanSiteLocationSpec{
					{
						Name:        "SiteB",
						ClusterName: "example-clusterb",
					},
				},
			},
		},
	},
}

var selfStaticXSiteErrorInfinispan = &ispnv1.Infinispan{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "example-clustera",
		Namespace: namespace,
	},
	Spec: ispnv1.InfinispanSpec{
		Service: ispnv1.InfinispanServiceSpec{
			Sites: &ispnv1.InfinispanSitesSpec{
				Local: ispnv1.InfinispanSitesLocalSpec{
					Name: "SiteA",
					Expose: ispnv1.CrossSiteExposeSpec{
						Type: ispnv1.CrossSiteExposeTypeClusterIP,
					},
				},
				Locations: []ispnv1.InfinispanSiteLocationSpec{
					{
						Name:        "SiteB",
						ClusterName: "example-clustera",
					},
				},
			},
		},
	},
}

var staticXSiteRemoteLocations = &ispnv1.Infinispan{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "example-clustera",
		Namespace: namespace,
	},
	Spec: ispnv1.InfinispanSpec{
		Service: ispnv1.InfinispanServiceSpec{
			Sites: &ispnv1.InfinispanSitesSpec{
				Local: ispnv1.InfinispanSitesLocalSpec{
					Name: "SiteA",
					Expose: ispnv1.CrossSiteExposeSpec{
						Type: ispnv1.CrossSiteExposeTypeClusterIP,
					},
				},
				Locations: []ispnv1.InfinispanSiteLocationSpec{
					{
						Name: "SiteC",
						URL:  "infinispan+xsite://example-clusterc-site:7901",
					},
					{
						Name: "SiteB",
						URL:  "infinispan+xsite://example-clusterb-site",
					},
				},
			},
		},
	},
}

var staticSiteService = &corev1.Service{
	ObjectMeta: metav1.ObjectMeta{
		Name:      staticXSiteInfinispan.GetSiteServiceName(),
		Namespace: namespace,
		Labels:    infinispan.LabelsResource(staticXSiteInfinispan.Name, "infinispan-service"),
	},
	Spec: corev1.ServiceSpec{
		Type:     corev1.ServiceTypeClusterIP,
		Selector: infinispan.ServiceLabels(staticXSiteInfinispan.Name),
		Ports: []corev1.ServicePort{
			{
				Port:       consts.CrossSitePort,
				TargetPort: intstr.IntOrString{IntVal: consts.CrossSitePort},
			},
		},
	},
}

var logger = logf.Log.WithName("xiste-test")

func TestComputeXSiteStatic(t *testing.T) {
	xsite, err := ComputeXSite(staticXSiteInfinispan, nil, staticSiteService, logger, nil)
	assert.Nil(t, err)

	assert.Equal(t, staticXSiteInfinispan.Spec.Service.Sites.Local.Name, xsite.Name, "Local site name")
	assert.Equal(t, staticXSiteInfinispan.GetSiteServiceName(), xsite.Address, "Local site address")
	assert.Equal(t, int32(consts.CrossSitePort), xsite.Port, "Local site port")

	assert.Equal(t, 2, len(xsite.Backups), "Backup sites number")
	assert.Contains(t, xsite.Backups, configuration.BackupSite{Address: "example-clusterb-site", Name: "SiteB", Port: int32(consts.CrossSitePort)}, "Backup SiteB contains")
	assert.Contains(t, xsite.Backups, configuration.BackupSite{Address: "example-clusterc-site", Name: "SiteC", Port: int32(consts.CrossSitePort + 1)}, "Backup SiteC contains")
}

func TestComputeXSiteSelfStatic(t *testing.T) {
	xsite, err := ComputeXSite(selfStaticXSiteInfinispan, nil, staticSiteService, logger, nil)
	assert.Nil(t, err)

	assert.Equal(t, 1, len(xsite.Backups), "Backup sites number")
	assert.Equal(t, fmt.Sprintf("%s.%s.svc.cluster.local", "example-clusterb-site", namespace), xsite.Backups[0].Address, "Backup site address")
	assert.Equal(t, int32(consts.CrossSitePort), xsite.Backups[0].Port, "Backup site port")
}

func TestComputeXSiteSelfStaticError(t *testing.T) {
	_, err := ComputeXSite(selfStaticXSiteErrorInfinispan, nil, staticSiteService, logger, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unable to link the cross-site service with itself")

}

func TestGetSiteLocationsName(t *testing.T) {
	assert.ElementsMatch(t, []string{"SiteA", "SiteB", "SiteC"}, staticXSiteInfinispan.GetSiteLocationsName(), "Site locations")
	assert.ElementsMatch(t, []string{"SiteA", "SiteB", "SiteC"}, staticXSiteRemoteLocations.GetSiteLocationsName(), "Only remote site locations")

	assert.Equal(t, 2, len(staticXSiteInfinispan.GetRemoteSiteLocations()), "Remote site locations count")
	assert.Equal(t, "SiteB", staticXSiteInfinispan.GetRemoteSiteLocations()["SiteB"].Name, "Remote site locations (SiteB)")
	assert.Equal(t, "SiteC", staticXSiteInfinispan.GetRemoteSiteLocations()["SiteC"].Name, "Remote site locations (SiteC)")

	assert.Equal(t, 2, len(staticXSiteRemoteLocations.GetRemoteSiteLocations()), "Remote site locations count")
	assert.Equal(t, "SiteB", staticXSiteRemoteLocations.GetRemoteSiteLocations()["SiteB"].Name, "Remote site locations (SiteB)")
	assert.Equal(t, "SiteC", staticXSiteRemoteLocations.GetRemoteSiteLocations()["SiteC"].Name, "Remote site locations (SiteC)")
}
