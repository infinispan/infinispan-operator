module github.com/infinispan/infinispan-operator

go 1.12

require (
	cloud.google.com/go v0.36.0 // indirect
	github.com/docker/spdystream v0.0.0-20181023171402-6480d4af844c // indirect
	github.com/elazarl/goproxy v0.0.0-20191011121108-aa519ddbe484 // indirect
	github.com/go-logr/logr v0.1.0
	github.com/go-logr/zapr v0.1.0 // indirect
	github.com/golang/groupcache v0.0.0-20190129154638-5b532d6fd5ef // indirect
	github.com/google/btree v1.0.0 // indirect
	github.com/google/uuid v1.1.0 // indirect
	github.com/googleapis/gnostic v0.2.0 // indirect
	github.com/gregjones/httpcache v0.0.0-20190212212710-3befbb6ad0cc // indirect
	github.com/hashicorp/golang-lru v0.5.0 // indirect
	github.com/imdario/mergo v0.3.7 // indirect
	github.com/mattbaird/jsonpatch v0.0.0-20171005235357-81af80346b1a // indirect
	github.com/onsi/ginkgo v1.10.3 // indirect
	github.com/onsi/gomega v1.7.1 // indirect
	github.com/operator-framework/operator-sdk v0.4.0
	github.com/pborman/uuid v0.0.0-20180906182336-adf5a7427709 // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/prometheus/client_golang v0.9.2 // indirect
	github.com/prometheus/client_model v0.0.0-20190129233127-fd36f4220a90 // indirect
	github.com/prometheus/common v0.2.0 // indirect
	github.com/prometheus/procfs v0.0.0-20190219184716-e4d4a2206da0 // indirect
	go.uber.org/atomic v1.3.2 // indirect
	go.uber.org/multierr v1.1.0 // indirect
	go.uber.org/zap v1.9.1 // indirect
	golang.org/x/oauth2 v0.0.0-20190220154721-9b3c75971fc9 // indirect
	gopkg.in/yaml.v2 v2.2.4
	k8s.io/api v0.0.0-20181126151915-b503174bad59
	k8s.io/apiextensions-apiserver v0.0.0-20181126155829-0cd23ebeb688
	k8s.io/apimachinery v0.0.0-20181126123746-eddba98df674
	k8s.io/client-go v0.0.0-20181126152608-d082d5923d3c
	k8s.io/code-generator v0.17.0 // indirect
	sigs.k8s.io/controller-runtime v0.1.8
	sigs.k8s.io/testing_frameworks v0.1.2 // indirect
)

// Workaround for some areas where gonum.org is blocked
replace gonum.org/v1/gonum => github.com/gonum/gonum v0.6.0

replace ./gonum.org/v1/netlib => ./github.com/gonum/netlib
