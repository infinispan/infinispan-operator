# ![Infinispan Operator](./infinispan_operator_stacked.png)
[![License](https://img.shields.io/github/license/infinispan/infinispan?style=for-the-badge&logo=apache)](https://www.apache.org/licenses/LICENSE-2.0)
[![Project Chat](https://img.shields.io/badge/zulip-join_chat-brightgreen.svg?style=for-the-badge&logo=zulip)](https://infinispan.zulipchat.com/)


The **Infinispan Operator** is the official Kubernetes operator for managing Infinispan clusters. 
It automates the deployment, scaling, and lifecycle management of Infinispan instances using Custom Resource Definitions (CRDs). 
With the Infinispan Operator, users can easily create, configure, and monitor Infinispan distributed in-memory database in a 
Kubernetes environment, ensuring high availability and resilience for their applications.

## System Requirements

* [Golang 1.21](https://github.com/golang/go) or higher.
* [Docker](https://www.docker.com/) or [Podman](https://podman.io/)
* [Operator SDK 1.24.1](https://github.com/operator-framework/operator-sdk/releases/tag/v1.24.1)
* A running [Kubernetes](https://kubernetes.io/) cluster

## Usage Documentation
For details on **how to use** the operator, please read the **[official Infinispan Operator documentation](https://infinispan.org/docs/infinispan-operator/main/operator.html)**.

# Developer Guide
This guide is intended for developers who wish to build and contribute to the Operator.

## Kubernetes
A Kubernetes cluster is necessary to develop and run the Operator. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for 
testing, or run against a remote cluster.

**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Setting a Local Kubernetes Cluster using Docker and Kind
In the scripts folder, you will find scripts to create a local KIND cluster backed by a local [Docker](https://www.docker.com/)
repository and integrated with the Operator Lifecycle Manager ([OLM](https://olm.operatorframework.io/)).

First run
```sh
./scripts/ci/kind-with-olm.sh
```

Then install [cert-manager](https://cert-manager.io) on it:

```sh
make deploy-cert-manager
```

### Podman and Podman Desktop
If you are using Windows or Mac, you can use [Podman](https://podman.io/) and [Podman Desktop](https://podman-desktop.io/)
as alternative tools to create a local Kubernetes cluster for development purposes, allowing you to set up a Kind,
Microshift, or OpenShift Kubernetes cluster.

## Skaffold
We utilise [Skaffold](https://skaffold.dev/) to drive CI/CD, so you will need to download the latest binary in order to
follow the steps below.

## Development
Build the Operator image and deploy it to a cluster:

```sh
skaffold dev
```

The `dev` command will make the process follow the logs produced by the remote process.
Exiting from the process, the image will be undeployed from the cluster.
See more on [Skaffold Documentation](https://skaffold.dev/docs/).

Changes to the local `**/*.go` files will result in the image being rebuilt and the Operator deployment updated.

## Testing

### Unit Tests
Run all the unit tests by calling:

```sh
make test
```

### Integration Tests

The different categories of integration tests can be executed with the following commands:

- `make infinispan-test`
- `make cache-test`
- `make batch-test`
- `make multinamespace-test`
- `make backuprestore-test`

The target cluster should be specified by exporting or explicitly providing `KUBECONFIG`, e.g. `make infinispan-test KUBECONFIG=/path/to/admin.kubeconfig`.

#### Env Variables
The following variables can be exported or provided as part of the `make *test` call.

| Variable              | Purpose                                                                            |
|-----------------------|------------------------------------------------------------------------------------|
| `TEST_NAME`           | Specify a single test to run                                                       |
| `TESTING_NAMESPACE`   | Specify the namespace/project for running test                                     |
| `RUN_LOCAL_OPERATOR`  | Specify whether run operator locally or use the predefined installation            |
| `EXPOSE_SERVICE_TYPE` | Specify expose service type. `NodePort \| LoadBalancer \| Route`.                  |
| `EXPOSE_SERVICE_HOST` | Specify the service host. Useful to pass `localhost`.                          |
| `PARALLEL_COUNT`      | Specify parallel test running count. Default is one, i.e. no parallel tests enabled. |

The following command runs a single integration test called `TestBaseFunctionality`:

```sh
 make infinispan-test TEST_NAME=TestBaseFunctionality
````

### Xsite
Cross-Site tests require you to create two k8s Kind clusters or utilize already prepared OKD clusters:
```
$ source scripts/ci/configure-xsite.sh $KIND_VERSION $METALLB_VERSION
```

To test locally in running Kind clusters, run:
```
$ go test -v ./test/e2e/xsite/ -timeout 30m
```

## Arm Support
In order to test on ARM machines locally, it's necessary for a few changes to be made.

1. `scripts/ci/kind.sh` needs to be executed with the following env variables:

```bash
DOCKER_REGISTRY_IMAGE="registry:2"
KINDEST_IMAGE="kindest/node"
KINDEST_NODE_VERSION="v1.28.7" # Must be >= 1.28.x to ensure ARM support works as expected
```

2. When executing `make infinispan-test`, set `TEST_NGINX_IMAGE="nginx"` so a multi-arch nginx container is used.

## Debugging
Build the Operator image with [dlv](https://github.com/go-delve/delve) so that a remote debugger can be attached
to the Operator deployment from your IDE.

```sh
skaffold debug
```

## Deploying
Build the Operator image and deploy to a cluster:

```sh
skaffold run
```

The `run` command returns immediately after the deploy.
The image won't be undeployed from the cluster.
See more on [Skaffold Documentation](https://skaffold.dev/docs/).

## Remote Repositories
The `skaffold dev|debug|run` commands can all be used on a remote k8s instance, as long as the built images are accessible
on the cluster. To build and push the operator images to a remote repository, add the `--default-repo` option, for example:

```sh
skaffold run --default-repo <remote_repo>
```

## OLM Bundle
The OLM bundle manifests are created by executing `make bundle VERSION=<latest-version>`.

This will create a `bundle/` dir in your local repository containing the bundle metadata and manifests, as well as a
`bundle.Dockerfile` for generating the image.

The bundle image can be created and pushed to a repository with:

```
make bundle-build bundle-push VERSION=<latest-version> IMG=<operator-image> BUNDLE_IMG=<bundle-image>
```

## Operator Version
The next version of the Operator to be released is stored in the `./version.txt` file at the root of 
the project. The contents of this file are used to control the generation of documentation and 
other resources. **Update this file after each Operator release**.

## Add a new Infinispan Operand
1. Call the "Add Operand" GitHub Action

# Release
Follow these steps to release the Infinispan Operator:

1. Tag the release `git tag <x.y.z>` and push to GitHub 
2. Create and push the multi-arch image using the created tag via the "Image Release" GitHub Action
3. Remove the old bundle from local `rm -rf bundle`
4. Create OLM bundle `make bundle VERSION=<x.y.z> CHANNELS=stable DEFAULT_CHANNEL=stable IMG=quay.io/infinispan/operator:<x.y.z>.Final`
5. Copy contents of `bundle/` and issue PRs to:
    - https://github.com/k8s-operatorhub/community-operators
    - https://github.com/redhat-openshift-ecosystem/community-operators-prod
6. Once PR in 5 has been merged and Operator has been released to OperatorHub, update the "replaces" field in `config/manifests/bases/infinispan-operator.clusterserviceversion.yaml`
to `replaces: infinispan-operator.v<x.y.z>`
7. **Update the `version.txt` file to the next release version**
8. Update `scripts/ci/install-catalog-source.sh` `VERSION` field to the next release version
9. Update `scripts/create-olm-catalog.sh` to include the just released version in `BUNDLE_IMGS` and the next release version in the update graph
10. Commit changes with appropriate commit message, e.g "Next Version <x.y.z>"