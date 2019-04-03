## Infinispan Operator

[![Build Status](https://travis-ci.org/infinispan/infinispan-operator.svg?branch=master)](https://travis-ci.org/infinispan/infinispan-operator)

This is an Openshift operator to run and rule Infinispan.

### System Requirements

* [go](https://github.com/golang/go) with `$GOPATH` set to `$HOME/go`
* Docker
* [dep](https://github.com/golang/dep#installation)    
* A running [OKD cluster](https://www.okd.io/download.html) with `system:admin` access.

### Building the Infinispan Operator

1. Add the source under `$GOPATH`:
```
$ git clone https://github.com/infinispan/infinispan-operator.git $GOPATH/src/github.com/infinispan/infinispan-operator
```
2. Change to the source directory.
```
$ cd $GOPATH/src/github.com/infinispan/infinispan-operator
```
3. Review the available build targets.
```
$ make
```
4. Run any build target. For example, compile and build the Infinispan Operator with:
```
$ make build
```

### Running the Infinispan Operator

1. Start OKD. For example:
```
$ oc cluster up
```
2. Specify the Kube configuration for the OKD cluster.
```
$ export KUBECONFIG=/path/to/admin.kubeconfig
```
3. Create the Infinispan operator on OKD.
  * Use the public  [jboss/infinispan-operator](https://hub.docker.com/r/jboss/infinispan-operator) image:
  ```
  $ make run
  ```
  * Use a locally built image:
  ```
  $ make run-local
  ```
4. Open a new terminal window and create an Infinispan cluster with three nodes:
```
$ oc apply -f deploy/cr/cr_minimal.yaml
infinispan.infinispan.org/example-infinispan configured
```
5. Watch the pods start until they start running.
```
$ oc get pods -w
NAME                                   READY     STATUS              RESTARTS   AGE
example-infinispan-54c66fd755-28lvx    0/1       ContainerCreating   0          4s
example-infinispan-54c66fd755-7c4zc    0/1       ContainerCreating   0          4s
example-infinispan-54c66fd755-8gbxf    0/1       ContainerCreating   0          5s
infinispan-operator-69d7d4469d-f62ws   1/1       Running             0          3m
example-infinispan-54c66fd755-8gbxf    1/1       Running             0          8s
example-infinispan-54c66fd755-7c4zc    1/1       Running             0          8s
example-infinispan-54c66fd755-28lvx    1/1       Running             0          8s
```

Now it's time to have some fun. Let's see the Infinispan operator in action.

Change the cluster size in `deploy/cr/cr_minimal.yaml` and then apply it again. The Infinispan operator scales the number of nodes in the cluster up or down.

### Custom Resource Definitions
The Infinispan Operator creates clusters based on custom resource definitions that specify the number of nodes and configuration to use.

Infinispan resources are defined in [infinispan-types.go](https://github.com/infinispan/infinispan-operator/blob/master/pkg/apis/infinispan/v1/infinispan_types.go).

#### Minimal Configuration
Creates Infinispan clusters with `cloud.xml` that uses the Kubernetes JGroups stack to form clusters with the `KUBE_PING` protocol.

```yaml
apiVersion: infinispan.org/v1
kind: Infinispan
metadata:
  # Sets a name for the Infinispan cluster.
  name: example-infinispan-minimal
spec:
  # Sets the number of nodes in the cluster.
  size: 3
```

#### Infinispan Configuration
Creates Infinispan clusters using `clustered.xml` in the `/opt/jboss/infinispan-server/standalone/configuration/` directory on the image.

```yaml
apiVersion: infinispan.org/v1
kind: Infinispan
metadata:
  # Sets a name for the Infinispan cluster.
  name: example-infinispan-config
config:
  name: clustered.xml
spec:
  # Sets the number of nodes in the cluster.
  size: 3
```

#### Custom Configuration
Creates Infinispan clusters with custom configuration through the ConfigMap API.

```yaml
apiVersion: infinispan.org/v1
kind: Infinispan
metadata:
  # Sets a name for the Infinispan cluster.
  name: example-infinispan-custom
config:
  sourceType: ConfigMap
  # Specifies the name of the ConfigMap.
  sourceRef:  my-config-map
  # Specifies the custom configuration file.
  name: my-config.xml
spec:
  # Sets the number of nodes in the cluster.
  size: 3
```

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

Submissions to [operatorhub.io](https://operatorhub.io/) fall into 2 categories:
upstream and community.

Upstream submissions should work on vanilla Kubernetes (e.g. Minikube),
whereas community submissions should work on OpenShift
(see [here](https://github.com/operator-framework/community-operators) for minimum requirements). 

Testing submissions is a two-step process:
First, you need to push the operator to a quay.io application repository.
Details on this will be provided ASAP.

Then, you need to test them on either Minikube or OpenShift depending on the submissions.
Next sessions explain how to test each.

**NOTE:** All testing included below assumes that the operator will be installed in the `local-operators` namespace.
If you want to use a different namespace, both YAML descriptor files and `make` calls need to be adjusted.
For the descriptors, simply replace `local-operators` for your desired namespace.
For the scripts, add the desired namespace as env variable, e.g. `make NAMESPACE=<name> ....`.


#### Upstream and Minikube

The `operatorhub/minikube` folder contains a `Makefile` and a series of scripts to help achieve this.
With `minikube` in your path, type:

```bash
$ cd operatorhub/minikube
$ make all
```

This command will trigger the creation of a new `minikube` profile, 
with the optimal configuration for testing the Infinispan operator.
Next, it installs Operator Lifecycle Manager (OLM) and Operator Marketplace components.
These components are necessary for the operator to run. 
Then, the operator gets installed and a sample Infinispan cluster is instantiated.

This process is not yet failproof due to the asynchronous nature of Kubernetes.
So, while executing the the script you might encounter `no matches for kind` errors.

These kind of errors mean the installation of Kubernetes elements did not complete.
This can sometimes happen when installation of descriptors happens too quickly for changes to take effect.
To solve the issue, identify the make target that failed and re-execute it.

Once you re-execute the failing target you might see errors saying that some elements already exist, you can ignore those.
Do verify that all pods are running before continuing with the process.
This can be done with `kubectl get pods --all-namespaces`.

Once all pods are running, look at the `Makefile` and detect which targets are still to run.

E.g. this is an example where target `make install-olm` did not complete:

```bash
+ kubectl create -f deploy/upstream/manifests/latest/
unable to recognize "deploy/upstream/manifests/latest/0000_50_olm_11-olm-operators.catalogsource.yaml": no matches for kind "CatalogSource" in version "operators.coreos.com/v1alpha1"
```

E.g. this an example where the target `make install-operator` did not complete:

```bash
+ kubectl apply -f https://raw.githubusercontent.com/infinispan/infinispan-operator/0.2.1/deploy/cr/cr_minimal.yaml -n local-operators
error: unable to recognize "https://raw.githubusercontent.com/infinispan/infinispan-operator/0.2.1/deploy/cr/cr_minimal.yaml": no matches for kind "Infinispan" in version "infinispan.org/v1"
```

To be able to run the test that instantiates an Infinispan cluster with the operatorhub submission,
the Infinispan CRD needs to be installed and the infinispan-operator needs to be running, e.g.

```bash
$ kubectl get crd
NAME                                          CREATED AT
infinispans.infinispan.org                    2019-04-03T09:15:48Z
```

```bash
$ kubectl get pods --all-namespaces
NAMESPACE         NAME                                   READY   STATUS    RESTARTS   AGE
local-operators   infinispan-operator-5549f446f-9mqkp    1/1     Running   0          44s
```

#### Community and OpenShift 4

The `operatorhub/openshift4` folder contains a `Makefile` and a series of scripts to help achieve this.

**NOTE:** This section assumes you have a running OpenShift 4 cluster,
an OpenShift client configured to talk to it,
and you are using an administrator account.

OpenShift 4 already comes with Operator Lifecycle Manager (OLM) and Operator Marketplace components installed,
so it's not necessary to install them again.

Testing on OpenShift 4 can be done automatically or manually:

* In the automatic test, the operator is added to the operator hub,
then it's installed in the defined namespace,
and tested all in one step:
`make all`

* In the manual test, `make install-operatorsource` is called to add the operator to the operator hub.
Installing the operator into the desired namespace is done via the Operator Hub user interface in the OpenShift console.
Finally, the operator can be tested by calling `make test`.

#### Community and OpenShift 3.11

TODO


### Releases
To create releases, run:
```
$ make DRY_RUN=false RELEASE_NAME=X.Y.Z release
```
