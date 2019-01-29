## Infinispan Server Operator

This is an Openshift operator to run and rule Infinispan.

### Build instructions

Requirements:

* go (GOPATH=$HOME/go)  
* [dep](https://github.com/golang/dep#installation)    
* [operator-sdk](https://github.com/operator-framework/operator-sdk/) v0.4.0    


```
src git clone https://github.com/jboss-dockerfiles/infinispan-server-operator.git $GOPATH/src/github.com/jboss-dockerfiles/infinispan-server-operator
cd $GOPATH/src/github.com/jboss-dockerfiles/infinispan-server-operator

```

Run ```dep ensure``` the first time to download dependencies.


### Running the operator locally

To launch the operator locally, follow the steps:

```
oc cluster up
oc login -u system:admin
oc create configmap infinispan-app-configuration --from-file=./config  # this creates the configmap needed by the cluster  
oc apply -f deploy/rbac.yaml # this defines the accounts and roles
oc apply -f deploy/crds/infinispan_v1_infinispan_crd.yaml # this defines the resource  
operator-sdk up local --namespace=myproject  
```

In another term:
```
oc apply -f deploy/crds/infinispan_v1_infinispan_cr.yaml # this creates the cluster
```


you can have fun and change the size parameter in infinispan_v1_infinispan_cr.yaml and apply it again to see the operator in action  

### Building and pushing the image

```
operator-sdk build jboss/infinispan-server-operator

docker push jboss/infinispan-server-operator
```

### Running inside the cluster

To run the operator inside Openshift, using the public image [jboss/infinispan-server-operator](https://hub.docker.com/r/jboss/infinispan-server-operator) :

```
oc create configmap infinispan-app-configuration --from-file=./config

oc apply -f deploy/rbac.yaml
oc apply -f deploy/crds/infinispan_v1_infinispan_crd.yaml
oc apply -f deploy/operator.yaml
oc apply -f deploy/crds/infinispan_v1_infinispan_cr.yaml
```

