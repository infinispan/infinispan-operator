# infinispan-operator
This is an openshift operator to run and manage Infinispan cluster.
It has been developed following the tutorial [here](https://github.com/operator-framework/operator-sdk/blob/master/doc/user-guide.md) and at the moment it's ok to follow that guide if you want to do experiments  on this code. Please share you contribute if you think you developed some cool features.

### Setup
vscode (or your favorite golang IDE)   
go (GOPATH=$HOME/go)  
operator-sdk (master)  
oc cluster up (okd 3.11)  

### Quickstart
open 2 terminals  
set PATH for go and okd  

term 1  
oc cluster up  
oc apply -f deploy/crds/cache_v1alpha1_infinispan_crd.yaml # this defines the resurce  

term 2  
operator-sdk up local --namespace=&lt;yournamespace&gt;  

term 1  
oc create configmap infinispan-app-configuration --from-file=./config  # this creates the configmap needed by the cluster  
oc apply -f deploy/crds/cache_v1alpha1_infinispan_cr.yaml # this creates the cluster

you can have fun and change the size parameter in cache_v1alpha1_infinispan_cr.yaml and apply it again to see the operator in action  

### Buiding and pushing the image

```
cd $GOPATH  
mkdir -p $GOPATH/src/github.com/jboss-dockerfiles/  
cd $GOPATH/src/github.com/jboss-dockerfiles/  
git clone https://github.com/jboss-dockerfiles/infinispan-server-operator.git  
cd infinispan-server-operator  
operator-sdk build jboss/infinispan-server-operator  # Or other image name  

docker push jboss/infinispan-server-operator  # Or other image name  
```

### Running on an existing 4.0.0 cluster

After the image is pushed to a public repo, edit ```deploy/operator.yaml``` and replace REPLACE_IMAGE by the correct image name.

Then install the templates:
```
cd $GOPATH/src/github.com/jboss-dockerfiles/infinispan-operator
oc policy add-role-to-user view system:serviceaccount:$(oc project -q):default -n $(oc project -q)
oc create configmap infinispan-app-configuration --from-file=./config

oc apply -f deploy/service_account.yaml
oc apply -f deploy/role.yaml
oc apply -f deploy/role_binding.yaml
oc apply -f deploy/crds/cache_v1alpha1_infinispan_crd.yaml
oc apply -f deploy/operator.yaml
oc apply -f deploy/crds/cache_v1alpha1_infinispan_cr.yaml
