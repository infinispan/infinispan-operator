# Operator Lifecycle Manager (`OLM`) Integration

## Requirements

Testing OLM integration has the following requirements:

* A running OpenShift cluster, preferably 4.2 or higher.
* An account in [quay.io](https://quay.io/repository).
* [`jq`](https://stedolan.github.io/jq) command line tool.
* [`yq`](https://github.com/kislyuk/yq) command line utility.

## Interacting with Quay

OLM integration requires packages to be pushed to quay,
in order to evaluate correctness of integration.

To interact with Quay,
create a `.quay` containing your Quay username, e.g.

```bash
export QUAY_USERNAME=<...>
```

Apply the `.quay` file to export your Quay.io username:

```bash
$ source .quay
```

Next, generate a Quay token and export it,
so that it can be used for pushing test releases to Quay.
The script will wait for you type or paste your password,
which is necessary to generate the token:

```bash
$ source export-quay-token.sh
```

You can verify that the Quay token has been generated correctly by calling:

```bash
$ echo $QUAY_TOKEN
basic Z3...
```

Next up, make sure you create an `infinispan` application repository in Quay,
so that releases can be pushed there.

## Delete existing OLM set up

To test a custom Quay repository,
it's recommended to remove any existing OLM set up to avoid conflicts:

```bash
$ make clean-olm
```

## Create Infinispan Cluster with custom Quay repository

As a first step, 
let's try to create a basic Infinispan cluster,
out of OLM descriptors pushed to a custom Quay repository.
Please make sure you have executed steps in 
[Interacting with Quay](#interacting-with-quay), 
before continuing with this section.

Pushing a release to Quay requires a new version to be given each time,
independent of the operator version.
To avoid having to explicitly provide a new version for each release,
the automation present here takes the latest version pushed and increments it by one.

So, if you've just created the Quay application repository for the first time,
or you're running these tests for the first time,
do an initial manual release with a version number that can easily be incremented.
For example:

```bash
$ QUAY_VERSION=0.0.100 make manual-push-quay
```

Once you have created this initial version,
calls to create or update the cluster will increment that version as needed.

Next, let's actually create create a cluster:

```bash
$ make create-cluster
```

When the make call completes, 
an Infinispan cluster should be running as well as an instance of the Infinispan operator:

```bash
$ oc get pods
NAME                                   READY   STATUS    RESTARTS   AGE
example-infinispan-0                   1/1     Running   0          20m
infinispan-operator-7d8d49757f-8662p   1/1     Running   0          24m
```

You can verify the Infinispan server version running:

```bash
$ oc logs example-infinispan-0 | grep started
06:56:38,837 INFO  [org.infinispan.SERVER] (main) ISPN080001: Infinispan Server 10.0.0.Final started in 9497ms
```

Aside from from starting the cluster, 
`create-cluster` also creates a cache that that persists to file store.
Then, it stores some data and retrieves to verify that the data was stored correctly.

By creating a cache that stores data to a file store,
we can verify that when the upgrade occurs the data still remains.
The next section focuses on this.

## Upgrade Operator

The aim of this section is to simulate an Infinispan operator upgrade,
and how that affects existing Infinispan clusters that were created by the operator being upgraded.

**NOTE:** Before running this section,
make you have created a running Infinispan cluster following steps in previous sections.

To simulate an upgrade,
the test will create a brand new operator manifest release based on the latest release.
To easily differentiate both releases, the new release will use the latest Infinispan server release available:

```bash
$ make upgrade-cluster
```

Once the upgrade has fired up,
it will take a couple of minutes for OpenShift to detect that a new operator was released.
Then, it will stop the current operator pod and will fire a new one with the new operator.
The new operator then inspects any existing cluster and decides whether it needs to upgrade it.
In this particular case it will, and once it has upgraded it you'll be able to see it running a different Infinispan version:

```bash
$ oc logs example-infinispan-0 | grep started
14:11:40,967 INFO  [org.infinispan.SERVER] (main) ISPN080001: Infinispan Server 10.1.0.Final started in 24496ms
```

**NOTE**: OpenShift automatically updates the operator with the new release
because of the way the operator subscription has been configured
(see `olm-subscription.yaml#.spec.installPlanApproval`).
Users can configure that to be manual,
in which case they'd explicitly allow the update to happen.

Once the upgraded Infinispan cluster is running,
you can verify that the data initially stored survived the upgrade:

```bash
$ make get
...
< HTTP/1.1 200 OK
test-value
```

## Cleaning up

If there are any issues along the way,
or you just want to delete any created resources,
simply call:

```bash
$ make delete-cluster
```

## Testing local changes

A successful upgrade requires the operator to cooperate with OpenShift.
Hence, it's likely you might want to try out or modify the upgrade logic.
To try out different upgrade logic,
build an operator image,
and push it to a Docker repository that you control.
By providing a custom `IMAGE_REPO` value that maps to a Docker username, 
the operator image can be pushed to a different organization:

```bash
IMAGE_REPO=<...> make image push-docker
```

Once the custom operator image available is online,
the `upgrade-cluster` can be tweaked to upgrade to that operator image:

```bash
IMAGE_REPO=<...> make upgrade-cluster
```
