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

Custom resources allow you to dynamically configure Infinispan clusters using the Kubernetes API.

Infinispan resources are defined in [infinispan-types.go](https://github.com/infinispan/infinispan-operator/blob/master/pkg/apis/infinispan/v1/infinispan_types.go).

#### Minimal Configuration
Use this resource to create Infinispan clusters using the `cloud.xml` configuration. This is the default configuration and uses the Kubernetes JGroups stack to form clusters with the `KUBE_PING` protocol.

```yaml
apiVersion: infinispan.org/v1
kind: Infinispan
metadata:
  name: example-infinispan-minimal
spec:
  size: 3
```

#### Infinispan Configuration
Use this resource to create Infinispan clusters using a configuration such as `clustered.xml` from `/opt/jboss/infinispan-server/standalone/configuration/`.

```yaml
apiVersion: infinispan.org/v1
kind: Infinispan
metadata:
  name: example-infinispan-config
config:
  name: clustered.xml
spec:
  size: 3
```

#### Custom Configuration
Use this resource to create Infinispan clusters with custom configuration via a ConfigMap. For example, configure clusters with `my-config.xml` with a ConfigMap named `my-config-map`.

```yaml
apiVersion: infinispan.org/v1
kind: Infinispan
metadata:
  name: example-infinispan-custom
config:
  sourceType: ConfigMap
  sourceRef:  my-config-map
  name: my-config.xml
spec:
  size: 3
```
