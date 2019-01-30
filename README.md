## Infinispan Server Operator 

[![Build Status](https://travis-ci.org/jboss-dockerfiles/infinispan-server-operator.svg?branch=master)](https://travis-ci.org/jboss-dockerfiles/infinispan-server-operator)

This is an Openshift operator to run and rule Infinispan.

### Requirements


* go (GOPATH=$HOME/go)
* Docker
* [dep](https://github.com/golang/dep#installation)    
* An [OKD cluster](https://www.okd.io/download.html) 


### Build

Checkout sources under $GOPATH:
```
git clone https://github.com/jboss-dockerfiles/infinispan-server-operator.git $GOPATH/src/github.com/jboss-dockerfiles/infinispan-server-operator

cd $GOPATH/src/github.com/jboss-dockerfiles/infinispan-server-operator

```

The project has a self-documented Makefile. To see the available targets, execute ```make``` without any parameters.


```make build``` will compile the operator.


### Running the operator outside OKD


To launch the operator locally, pointing to a running OKD cluster, make sure OKD is started, e.g. with ```oc cluster up```. Then run:


```
make run-local
```

On other terminal, apply the resource template to instruct the operator to create a 3-node Infinispan cluster: 
```
oc apply -f deploy/crds/infinispan_v1_infinispan_cr.yaml
```

Check with ```oc get pods```.

You can have fun and change the size parameter in infinispan_v1_infinispan_cr.yaml and apply it again to see the operator in action.  

### Publishing the Docker image

```make push``` will build the image and push it to [Dockerhub](https://hub.docker.com/r/jboss/infinispan-server-operator). 

### Running the operator inside OKD

To run the operator inside OKD, using the public image [jboss/infinispan-server-operator](https://hub.docker.com/r/jboss/infinispan-server-operator) :

Make sure OKD is started, e.g. with ```oc cluster up```. Then run:

```
make run
```

To create a cluster, apply

```
oc apply -f deploy/crds/infinispan_v1_infinispan_cr.yaml
```

