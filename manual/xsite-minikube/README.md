# Requirements

* [Minikube](https://kubernetes.io/docs/tasks/tools/install-minikube) version 1.3.1 or higher.
* [`kubectl`](https://kubernetes.io/docs/tasks/tools/install-kubectl) version 1.15.2 or higher. 
* [yq](https://github.com/kislyuk/yq) command line utility.

# Testing

Configure Minikube so that 2 profiles are created,
one for each of the sites being tested:

```bash
$ make config
✅  minikube profile was successfully set to SiteA
...
✅  minikube profile was successfully set to SiteB
...
```

Start Minikube profiles representing independent sites:

```bash
$ make start
✅  minikube profile was successfully set to SiteA
...
🏄  Done! kubectl is now configured to use "SiteA"

✅  minikube profile was successfully set to SiteB
...
🏄  Done! kubectl is now configured to use "SiteB"
```

If testing local operator changes,
build the operator image and push it to the docker daemons of each Minikube profile:

```
$ make image
✅  minikube profile was successfully set to SiteA
Sending build context to Docker daemon  141.5MB
...
Successfully tagged jboss/infinispan-operator:latest

✅  minikube profile was successfully set to SiteB
Sending build context to Docker daemon  141.5MB
... 
Successfully tagged jboss/infinispan-operator:latest
```

Next, deploy the operator and example operator instance to both sites:

```bash
$ make deploy-operator deploy
✅  minikube profile was successfully set to SiteA
...
deployment.extensions/infinispan-operator condition met

✅  minikube profile was successfully set to SiteB
...
deployment.extensions/infinispan-operator condition met

✅  minikube profile was successfully set to SiteA
...
infinispan.infinispan.org/example-infinispan created

✅  minikube profile was successfully set to SiteB
...
infinispan.infinispan.org/example-infinispan configured
```

Wait until the Infinispan server pods are up,
and the cluster has formed.

Next, create a cache in each site,
that backs up the data to the other site:

```bash
$ make create-cache
✅  minikube profile was successfully set to SiteA
curl .../rest/v2/caches/example
...
< HTTP/1.1 200 OK

✅  minikube profile was successfully set to SiteB
curl .../rest/v2/caches/example
...
< HTTP/1.1 200 OK
```

Store a key/value pair in the created cache in one of the sites:

```bash
make put
✅  minikube profile was successfully set to SiteA
curl -d 'test-value' .../rest/v2/caches/example/test-key
...
< HTTP/1.1 204 No Content
```

Finally, verify that the stored value is present in the other site:

```bash
make get
✅  minikube profile was successfully set to SiteB
curl .../rest/v2/caches/example/test-key
...
< HTTP/1.1 200 OK
...
test-value
```
