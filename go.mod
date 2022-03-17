module github.com/infinispan/infinispan-operator

go 1.15

require (
	github.com/GeertJohan/go.rice v1.0.2
	github.com/blang/semver v3.5.1+incompatible
	github.com/go-logr/logr v0.3.0
	github.com/go-playground/validator/v10 v10.8.0
	github.com/iancoleman/strcase v0.2.0
	github.com/onsi/gomega v1.14.0 // indirect
	github.com/openshift/api v3.9.0+incompatible
	github.com/operator-framework/api v0.4.0
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.44.0
	github.com/prometheus/common v0.26.0
	github.com/r3labs/sse/v2 v2.3.6
	github.com/stretchr/testify v1.7.0
	go.uber.org/zap v1.15.0
	gopkg.in/cenkalti/backoff.v1 v1.1.0
	gopkg.in/yaml.v2 v2.4.0
	honnef.co/go/tools v0.0.1-2020.1.3 // indirect
	k8s.io/api v0.19.4
	k8s.io/apiextensions-apiserver v0.19.4
	k8s.io/apimachinery v0.19.4
	k8s.io/client-go v0.19.4
	k8s.io/cloud-provider v0.19.4
	k8s.io/utils v0.0.0-20210722164352-7f3ee0f31471
	sigs.k8s.io/controller-runtime v0.7.0
	software.sslmate.com/src/go-pkcs12 v0.0.0-20210415151418-c5206de65a78
)

replace (
	github.com/go-playground/validator/v10 => github.com/go-playground/validator/v10 v10.8.0
	k8s.io/api => k8s.io/api v0.19.4
	k8s.io/apimachinery => k8s.io/apimachinery v0.19.4
	k8s.io/client-go => k8s.io/client-go v0.19.4
	k8s.io/client-go/plugin => k8s.io/client-go/plugin v0.19.4
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.7.0
)
