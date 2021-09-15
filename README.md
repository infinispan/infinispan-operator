# Infinispan Operator

This is a Kubernetes operator to manage Infinispan clusters.

## System Requirements

* [go 1.15](https://github.com/golang/go) or higher.
* Docker
* A running Kubernetes cluster

# Usage

For details on how to use the operator, please read the [official operator documentation](https://infinispan.org/docs/infinispan-operator/main/operator.html).

# Building the Operator

To build the go binary locally, execute:

`make manager`

## Docker Image

To create a docker container and push to a remote repository execute:

`make docker-build docker-push IMG=<image_name:tag>`


## Running the Operator

### Locally
The following commands will generate and install the CRDs to the local cluster before starting the operator. To only execute the operator, simply omit the `install` target.

To watch all namespaces:

`make install run`

To watch specific namespaces:

`make install run WATCH_NAMESPACE=namespace1,namespace2`

### On K8s
To deploy the operator to the cluster you're currently connected to, execute:

`make deploy IMG=<image_name:tag> DEPLOYMENT_NAMESPACE=<namespace>`

This will update the `config/manager/manager.yaml` to utilise the specified image and will create all of the required
resources on the kubernetes cluster in the `$DEPLOYMENT_NAMESPACE` namespace.

# OLM Bundle
The OLM bundle manifests are created by executing `make bundle VERSION=<latest-version>`.

This will create a `bundle/` dir in your local repositoray containing the bundle metadata and manifests, as well as a
`bundle.Dockerfile` for generating the image.

The bundle image can be created and pushed to a repository with:

```
make bundle-build bundle-push VERSION=<latest-version> IMG=<operator-image> BUNDLE_IMG=<bundle-image>
```

# Testing

## Unit Tests

`make unit-test`

## Go Integration Tests

The different categories of integration tests can be executed with the following commands:

- `make test`
- `make batch-test`
- `make multinamespace-test`
- `make backuprestore-test`

The target cluster should be specified by exporting or explicitly providing `KUBECONFIG`, e.g. `make test KUBECONFIG=/path/to/admin.kubeconfig`.

### Env Variables
The followin variables can be exported or provided as part of the `make *test` call.

| Variable              | Purpose                                                                              |
|-----------------------|--------------------------------------------------------------------------------------|
| `TEST_NAME`           | Specify a single test to run                                                         |
| `TESTING_NAMESPACE`   | Specify the namespace/project for running test                                       |
| `RUN_LOCAL_OPERATOR`  | Specify whether run operator locally or use the predefined installation              |
| `EXPOSE_SERVICE_TYPE` | Specify expose service type. `NodePort \| LoadBalancer \| Route`.                    |
| `PARALLEL_COUNT`      | Specify parallel test running count. Default is one, i.e. no parallel tests enabled. |

### Xsite
Cross-Site tests require you to create two k8s Kind clusters or utilize already prepared OKD clusters:
```
$ source scripts/ci/configure-xsite.sh $KIND_VERSION $METALLB_VERSION
```

Actual `$KIND_VERSION` and `$METALLB_VERSION` values can be explored inside the `Jenkinsfile` file 

To test locally in running Kind clusters, run:
```
$ go test -v ./test/e2e/xsite/ -timeout 30m
```

## Java Integration Tests
TODO
