## Infinispan Operator

[![Build Status](https://travis-ci.org/infinispan/infinispan-operator.svg?branch=2.0.x)](https://travis-ci.org/infinispan/infinispan-operator)

This is an OpenShift operator to run and rule Infinispan.

### System Requirements

* [go](https://github.com/golang/go)
* Docker
* A running [OKD cluster](https://www.okd.io/download.html) with `system:admin` access,
or a [Minikube cluster](https://kubernetes.io/docs/setup/minikube/).

### Building the Infinispan Operator

1. Add the source under any folder:
```
$ git clone https://github.com/infinispan/infinispan-operator.git
```
2. Change to the operator source directory.
```
$ cd ./infinispan-operator
```
3. Review the available build targets.
```
$ make
```
4. Run any build target. For example, compile and build the Infinispan Operator with:
```
$ make build
```
5. (Optional) The docker image can be build running:
```
$ make image IMAGE=image_name TAG=image_tag
```
or if your docker version doesn't have multistage build
```
$ make image IMAGE=image_name TAG=image_tag MULTISTAGE=NO
```
#### Cekit
There also a Cekit build environment under `build/cekit`
```
$ cekit build --overrides "{version: <version_default_is_latest>}" docker
```

### Running the Infinispan Operator

#### OpenShift

1. Start OKD. For example:
```
$ oc cluster up
```
2. Specify the Kube configuration for the OKD cluster.
```
$ export KUBECONFIG=/path/to/admin.kubeconfig
```
3. Create the Infinispan operator on OKD from the source code.
  * Use the public  [jboss/infinispan-operator](https://hub.docker.com/r/jboss/infinispan-operator) image:
  ```
  $ make run
  ```
  * It's possible to change a 'default' project name to the custom defined:
  ```
  $ export PROJECT_NAME=myproject
  ```
  * Use a locally built image:
  ```
  $ make run-local
  ```
4. Optionally install the Infinispan operator on OKD from the bundle file.
  * Create a new project or use an existing one:
  ```
  $ oc new-project infinispan-operator
  ```
  * If your project name is not `infinispan-operator`, download the Operator install bundle file and replace the pre-defined namespace. For example, the following command changes the project name to `my-infinispan-operator`:
  ```
  $ sed -i '/# Replace/{n;s/.*/  namespace: my-infinispan-operator/}' operator-install.yaml
  ```
  ```
  $ oc apply -f operator-install.yaml
  ```
  * If your project name is `infinispan-operator`, run this command to install the operator:
  ```
  $ oc apply -f https://raw.githubusercontent.com/infinispan/infinispan-operator/2.0.x/deploy/operator-install.yaml
  ```
5. Open a new terminal window and create an Infinispan cluster with two nodes:
```
$ oc apply -f https://raw.githubusercontent.com/infinispan/infinispan-operator/2.0.x/deploy/cr/minimal/cr_minimal.yaml
infinispan.infinispan.org/example-infinispan configured
```
6. Watch the pods until they are running.
```
$ oc get pods -w
NAME                                   READY     STATUS              RESTARTS   AGE
example-infinispan-54c66fd755-28lvx    0/1       ContainerCreating   0          4s
example-infinispan-54c66fd755-7c4zc    0/1       ContainerCreating   0          4s
infinispan-operator-69d7d4469d-f62ws   1/1       Running             0          3m
example-infinispan-54c66fd755-8gbxf    1/1       Running             0          8s
example-infinispan-54c66fd755-7c4zc    1/1       Running             0          8s
```

#### Minikube

1. Configure Minikube virtual machine. You only need to do this once:
```bash
$ make minikube-config
```
2. Start Minikube:
```bash
$ make minikube-start
```
3. Build the operator from the source code and run it locally:
```bash
$ make minikube-run-local
```
4. Optionally install the Infinispan operator on Kubernetes from the bundle file.
  * Create a new namespace or use an existing one:
  ```
  $ kubectl create namespace infinispan-operator
  ```
  * If your namespace name is not `infinispan-operator`, download the Operator install bundle file and replace the pre-defined namespace. For example, the following command changes the project name to `my-infinispan-operator`:
  ```
  $ sed -i '/# Replace/{n;s/.*/  namespace: my-infinispan-operator/}' operator-install.yaml
  ```
  ```
  $ kubectl apply -f operator-install.yaml
  ```
  * If your namespace name is `infinispan-operator`, run this command to install the operator:
  ```
  $ kubectl apply -f https://raw.githubusercontent.com/infinispan/infinispan-operator/2.0.x/deploy/operator-install.yaml
  ```
5. Open a new terminal window and create an Infinispan cluster with two nodes:
```bash
$ kubectl apply -f https://raw.githubusercontent.com/infinispan/infinispan-operator/2.0.x/deploy/cr/minimal/cr_minimal.yaml
```
6. Watch the pods until they are running
```bash
$ kubectl get pods -w
NAME                   READY   STATUS    RESTARTS   AGE
example-infinispan-0   1/1     Running   0          8m29s
example-infinispan-1   1/1     Running   0          5m53s
```

#### Next Steps

Now it's time to have some fun. Let's see the Infinispan operator in action.

Change the cluster size in `deploy/cr/minimal/cr_minimal.yaml` and then apply it again. The Infinispan operator scales the number of nodes in the cluster up or down.

### Custom Resource Definitions
The Infinispan Operator creates clusters based on custom resource definitions that specify the number of nodes and configuration to use.

Infinispan resources are defined in [infinispan-types.go](https://github.com/infinispan/infinispan-operator/blob/2.0.x/pkg/apis/infinispan/v1/infinispan_types.go).

#### Minimal Configuration
Creates Infinispan clusters:

```yaml
apiVersion: infinispan.org/v1
kind: Infinispan
metadata:
  # Sets a name for the Infinispan cluster.
  name: example-infinispan-minimal
spec:
  # Sets the number of nodes in the cluster.
  replicas: 3
```

### Further Configuration

Infinispan operator has other capabilities.
Check the
[official operator documentation](https://infinispan.org/infinispan-operator/2.0.x/documentation/asciidoc/titles/operator.html)
for more details.

### Testing the Infinispan Operator
Use the `test` target to test the Infinispan Operator on a specific OKD cluster.

To test a locally running cluster, run:
```
$ make test
```

Alternatively, pass `KUBECONFIG` to specify cluster access:
```
$ make test KUBECONFIG=/path/to/openshift.local.clusterup/openshift-apiserver/admin.kubeconfig
```

### Infinispan on OperatorHub.io
There are two categories for submissions to [operatorhub.io](https://operatorhub.io/), upstream and community.

* Upstream submissions should run on vanilla Kubernetes, such as Minikube.
* Community submissions should run on Red Hat OpenShift. See the  [community-operators](https://github.com/operator-framework/community-operators) site for minimum requirements.

Testing submissions is a two-step process:

1. Push the operator to a quay.io application repository. Details on this will be provided ASAP.
2. Test the operator on Minikube or OpenShift.
  * Upstream submissions, see [Testing Upstream Submissions with Minikube](#test_upstream).
  * Community submissions, see [Testing Community Submissions with OpenShift](#test_community).

 **Modifying the Namespace:** The test procedures in this `README` assume that the operator is installed in the `infinispan-operator` namespace.

 To use a different namespace, you must modify the YAML descriptor files and `make` calls. For descriptors, replace `infinispan-operator` with your desired namespace. For scripts, add your desired namespace as an environment variable; for example, `make NAMESPACE=my_namespace ${make_target}`.

<a name="test_upstream"></a>
#### Testing Upstream Submissions with Minikube
The `operatorhub/minikube` folder contains a `Makefile` and several `make` targets that you can use to test upstream submissions of the Infinispan Operator.

Minikube does not provide the Operator Lifecycle Manager (OLM) and Operator Marketplace components. You must install them before you can install and test the Infinispan Operator.

1. Ensure `minikube` is in your `$PATH`.
2. Run the appropriate `make` target. For example, run the `make all` target as follows:
  ```
  $ cd operatorhub/minikube
  $ make all
  ```

##### Specifying Hypervisor Drivers
The `Makefile` uses a `minikube` profile with an optimal configuration for testing the Infinispan Operator.

By default, the profile uses Virtual Box VM drivers. Use the `VMDRIVER` argument to specify a different hypervisor.

* `make VMDRIVER=${vm_driver_name} ${make_target}` to specify different VM drivers.
* `make VMDRIVER= ${make_target}` if you do not want to specify VM drivers (pass an empty value).

See the [Minikube docs](https://github.com/kubernetes/minikube) for information about supported hypervisors.

##### Make Targets

* `make all` runs each make target in sequence.
* `make test` instantiates an Infinispan cluster with the upstream submission. Note that the `make test` target requires the Infinispan CRD and the Infinispan Operator must be running.

  Check that the Infinispan CRD is installed:
  ```bash
  $ kubectl get crd
  NAME                                          CREATED AT
  infinispans.infinispan.org                    2019-04-03T09:15:48Z
  ```
  Check that the Infinispan Operator is running:
  ```bash
  $ kubectl get pods --all-namespaces
  NAMESPACE         NAME                                   READY   STATUS    RESTARTS   AGE
  infinispan-operator   infinispan-operator-5549f446f-9mqkp    1/1     Running   0          44s
  ```
* `make clean` removes the example Infinispan custom resource and Infinispan Operator from the Kubernetes cluster.
* `make delete` destroys the Minikube virtual machine.

##### Troubleshooting Errors
`no matches for kind` errors can occur when Kubernetes elements are not installed correctly, such as when descriptors are installed too quickly for changes to take effect.

First, you need to identify the `make` target that failed.

* Example where the `make install-olm` target did not complete:
  ```
  + kubectl create -f deploy/upstream/manifests/latest/
  unable to recognize \
  "deploy/upstream/manifests/latest/0000_50_olm_11-olm-operators.catalogsource.yaml": \
  no matches for kind "CatalogSource" in version "operators.coreos.com/v1alpha1"
  ```

* Example where the `make install-operator` target did not complete:
  ```
  + kubectl apply -f https://raw.githubusercontent.com/infinispan/infinispan-operator/0.2.1/deploy/cr/cr_minimal.yaml  \
  error: unable to recognize "https://raw.githubusercontent.com/infinispan/infinispan-operator/0.2.1/deploy/cr/cr_minimal.yaml": \
  no matches for kind "Infinispan" in version "infinispan.org/v1"
  ```

To resolve the error and continue testing, do the following:

1. Run the `make` target again. Note that you might notice errors about existing elements but you can ignore them.
2. Verify pods are running. For example, run:
  ```
  $ kubectl get pods --all-namespaces
  ```
3. Open the `Makefile` and determine which target is next in the sequence. For example, if errors occur for the `make install-olm` target, the next target in the sequence is `make checkout-marketplace`.
4. Manually run the remaining `make` targets.

<a name="test_community"></a>
#### Testing Community Submissions with OpenShift
Test community submissions of the Infinispan Operator with either OpenShift 4 or OpenShift 3.11.

##### Testing with OpenShift 4
The `operatorhub/openshift4` folder contains a `Makefile` and several `make` targets that you can use to test community submissions of the Infinispan Operator.

OpenShift 4 includes the Operator Lifecycle Manager (OLM) and Operator Marketplace components. You do not need to install them separately.

**Before You Begin:** You must have a running OpenShift 4 cluster with an `oc` client that is configured for the cluster. You must also be logged in with an administrator account.

Run the appropriate `make` target. For example, run the `make all` target as follows:
  ```
  $ cd operatorhub/openshift4
  $ make all
  ```

###### Make Targets

* `make all` adds the Infinispan Operator to the OperatorHub, installs it in the defined namespace, and then instantiates an Infinispan cluster with the community submission.
* `make install-operatorsource` adds the Infinispan Operator to the OperatorHub. You can then manually install the operator through the OperatorHub user interface in the OpenShift console.
* `make test` instantiates an Infinispan cluster with the community submission.
* `make clean` removes the example Infinispan custom resource and Infinispan Operator from the Kubernetes cluster.

### Releases
To create releases, run:
```
$ make DRY_RUN=false GITHUB_USERNAME=<...> REPLACES_RELEASE_NAME=N.M.O RELEASE_NAME=X.Y.Z SERVER_VERSION=a.b.c release
```

As a final step,
the release script creates two PRs in
[operatorhub.io](https://github.com/operator-framework/community-operators)
for the release to be included there.
Once the PRs have been created,
edit the description and add the PR form suggested
[here](https://github.com/operator-framework/community-operators),
marking those fields that apply.
