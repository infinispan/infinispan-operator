package config

import (
	"testing"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
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

type FakeEventLogger struct {
	logger logr.Logger
}

func (_ *FakeEventLogger) EventRecorder() *record.EventRecorder {
	return nil
}

func (fel *FakeEventLogger) Logger() *logr.Logger {
	return &fel.logger
}

var logger = logf.Log.WithName("xiste-test")

func TestComputeXSiteStatic(t *testing.T) {
	xsite, err := ComputeXSite(staticXSiteInfinispan, nil, staticSiteService, &FakeEventLogger{logger: logger})
	assert.Nil(t, err)

	assert.Equal(t, staticXSiteInfinispan.Spec.Service.Sites.Local.Name, xsite.Name, "Local site name")
	assert.Equal(t, staticXSiteInfinispan.GetSiteServiceName(), xsite.Address, "Local site address")
	assert.Equal(t, int32(consts.CrossSitePort), xsite.Port, "Local site port")

	assert.Equal(t, 2, len(xsite.Backups), "Backup sites number")
	assert.Equal(t, "SiteB", xsite.Backups[0].Name, "Backup site name")
	assert.Equal(t, "example-clusterb-site", xsite.Backups[0].Address, "Backup site address")
	assert.Equal(t, int32(consts.CrossSitePort), xsite.Backups[0].Port, "Backup site port")

	assert.Equal(t, "SiteC", xsite.Backups[1].Name, "Backup site name")
	assert.Equal(t, "example-clusterc-site", xsite.Backups[1].Address, "Backup site address")
	assert.Equal(t, int32(consts.CrossSitePort+1), xsite.Backups[1].Port, "Backup site port")

}

func TestGetSiteLocationsName(t *testing.T) {
	assert.ElementsMatch(t, []string{"SiteA", "SiteB", "SiteC"}, staticXSiteInfinispan.GetSiteLocationsName(), "Site locations")
	assert.ElementsMatch(t, []string{"SiteA", "SiteB", "SiteC"}, staticXSiteRemoteLocations.GetSiteLocationsName(), "Only remote site locations")

	assert.Equal(t, 2, len(staticXSiteInfinispan.GetRemoteSiteLocations()), "Remote site locations count")
	assert.Equal(t, "SiteB", staticXSiteInfinispan.GetRemoteSiteLocations()[0].Name, "Remote site locations (SiteB)")
	assert.Equal(t, "SiteC", staticXSiteInfinispan.GetRemoteSiteLocations()[1].Name, "Remote site locations (SiteC)")

	assert.Equal(t, 2, len(staticXSiteRemoteLocations.GetRemoteSiteLocations()), "Remote site locations count")
	assert.Equal(t, "SiteB", staticXSiteRemoteLocations.GetRemoteSiteLocations()[0].Name, "Remote site locations (SiteB)")
	assert.Equal(t, "SiteC", staticXSiteRemoteLocations.GetRemoteSiteLocations()[1].Name, "Remote site locations (SiteC)")
}
