# Requirements

* Access to two independent OpenShift clusters.
* Create a `.openshift` file with the credentials to access OpenShift clusters.
See [OpenShift Credentials](#openshift-credentials) section for details.
* Adjust yaml examples in `deploy/cr/xsite-openshift` folder,
with the corresponding backup site URLs.

# Testing

Apply the environment variables containing the OpenShift credentials:

```bash
$ source .openshift
```

Next, generate OpenShift session tokens to interact with OpenShift clusters:

```bash
$ source export-tokens.sh
```

Finally, deploy operator and instantiate the x-site Infinispan examples in each cluster:

```bash
$ make deploy-operator deploy
```

Once the operation completes, you can verify that the x-site forms by doing:

```bash
$ oc logs example-infinispan-0 | grep x-site
17:28:03,485 INFO  [org.infinispan.XSITE] (jgroups-5,example-infinispan-0-7160) ISPN000439: Received new x-site view: [SiteB]
17:28:22,235 INFO  [org.infinispan.XSITE] (jgroups-7,example-infinispan-0-7160) ISPN000439: Received new x-site view: [SiteB, SiteA]
```

Next, create a cache in each site that backs up the data to the other site:

```bash
$ make create-cache
curl ... /rest/v2/caches/example
...
HTTP/1.1 200 OK
...
curl ... /rest/v2/caches/example
...
HTTP/1.1 200 OK
```

Store a key/value pair in the created cache in one of the sites:

```bash
$ make put
curl -d 'test-value' .../rest/v2/caches/example/test-key
...
< HTTP/1.1 204 No Content
```

Finally, verify that the stored value is present in the other site:

```bash
$ make get
curl .../rest/v2/caches/example/test-key
...
< HTTP/1.1 200 OK
...
test-value
```

# Testing Local Operator Changes

If testing local operator code changes,
it's necessary to build and push the operator image to a location accessible by OpenShift clusters.

To do that, 
the scripts provided can be configured with an `IMAGE` environment variable,
which defines the image location for the operator.

This `IMAGE` environment variable can be used to build and push the image:

```bash
IMAGE=<...>/infinispan-operator make image push
```

And it can also be used to modify the operator deployed:

```bash
IMAGE=<...>/infinispan-operator make deploy-operator
```

# OpenShift Credentials

Credentials for OpenShift clusters should be provided via a `.openshift` file.
The files is expected to contain definitions for these environment variables:

* `SERVER_A`: OpenShift master URL in first cluster.
* `USERNAME_A`: Username to connect to first OpenShift cluster.
* `PASSWORD_A`: Password to connect to first OpenShift cluster.
* `SERVER_B`: OpenShift master URL in second cluster.
* `USERNAME_B`: Username to connect to second OpenShift cluster.
* `PASSWORD_B`: Password to connect to first OpenShift cluster.

Example:

```
export SERVER_A=https://api.<...>:6443
export USERNAME_A=<...>
export PASSWORD_A=<...>
export SERVER_B=https://api.<...>:6443
export USERNAME_B=<...>
export PASSWORD_B=<...>
```
