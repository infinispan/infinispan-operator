#!/usr/bin/env bash

. build/common.sh

export MAKE_DATADIR_WRITABLE=true
export GO111MODULE=on
export INITCONTAINER_IMAGE=quay.io/quay/busybox:latest
export TESTING_NAMESPACE=${TESTING_NAMESPACE-namespace-for-testing}
export KUBECONFIG=${KUBECONFIG-~/kind-kube-config.yaml}
KIND_VERSION=${1-${KIND_VERSION}}
METALLB_VERSION=${2-${METALLB_VERSION}}
YQ_VERSION=4.6.1

#Ensure that GOPATH/bin is in the PATH
if ! validateYQ; then
  printf "Valid yq not found in PATH. Trying adding GOPATH/bin\n"
  PATH=$(go env GOPATH)/bin:$PATH
  if ! validateYQ; then
    installYQ
  fi
fi

if ! [ -x "$(command -v kind)" ]; then
  curl -Lo kind https://github.com/kubernetes-sigs/kind/releases/download/"${KIND_VERSION}"/kind-linux-amd64 && chmod +x kind && mkdir -p /tmp/k8s/bin && mv kind /tmp/k8s/bin/
  PATH=/tmp/k8s/bin:$PATH
fi
if ! [ -x "$(command -v kubectl)" ]; then
  curl -LO https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl && chmod +x kubectl && mkdir -p /tmp/k8s/bin && mv kubectl /tmp/k8s/bin/
  PATH=/tmp/k8s/bin:$PATH
fi
# This requires for local (outside Travis CI) cleanup and run
kind delete clusters xsite1
kind delete clusters xsite2

# Common part for both nodes
./build/build.sh
docker build -t infinispan-operator . -f ./build/Dockerfile.single
yq ea -e 'del(.nodes[0].extraPortMappings)' kind-config.yaml > kind-config-xsite.yaml

for INSTANCE in "xsite1" "xsite2"; do
  kind create cluster --config kind-config-xsite.yaml --name "${INSTANCE}"
  kind load docker-image infinispan-operator --name "${INSTANCE}"
  ./build/install-crds.sh kubectl
  export TESTING_NAMESPACE_XSITE="${TESTING_NAMESPACE}"-"${INSTANCE}"
  kubectl create namespace "${TESTING_NAMESPACE_XSITE}"
  kubectl apply -f deploy/service_account.yaml -n "${TESTING_NAMESPACE_XSITE}"
  kubectl apply -f deploy/role.yaml -n "${TESTING_NAMESPACE_XSITE}"
  kubectl apply -f deploy/role_binding.yaml -n "${TESTING_NAMESPACE_XSITE}"
  kubectl apply -f deploy/clusterrole.yaml -n "${TESTING_NAMESPACE_XSITE}"
  yq ea -e '.subjects[0].namespace = strenv(TESTING_NAMESPACE_XSITE)' deploy/clusterrole_binding.yaml | kubectl apply -n "${TESTING_NAMESPACE_XSITE}" -f -
  yq ea -e '.spec.template.spec.containers[0].image = "infinispan-operator" | .spec.template.spec.containers[0].imagePullPolicy="Never"' deploy/operator.yaml | kubectl apply -n "${TESTING_NAMESPACE_XSITE}" -f -

  # Creating service account for the cross cluster token based auth
  kubectl create clusterrole xsite-cluster-role --verb=get,list,watch --resource=nodes,services
  kubectl create serviceaccount "${INSTANCE}" -n "${TESTING_NAMESPACE_XSITE}"
  kubectl create clusterrolebinding xsite-cluster-role-binding --clusterrole=xsite-cluster-role --serviceaccount="${TESTING_NAMESPACE_XSITE}":"${INSTANCE}"

  # Configuring MetalLB (https://metallb.universe.tf/) for real LoadBalancer setup on the Kind
  kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/"${METALLB_VERSION}"/manifests/namespace.yaml
  kubectl create secret generic -n metallb-system memberlist --from-literal=secretkey="$(openssl rand -base64 128)"
  wget -q -O - https://raw.githubusercontent.com/metallb/metallb/"${METALLB_VERSION}"/manifests/metallb.yaml | sed "s|image: metallb/|image: quay.io/metallb/|" | kubectl apply -f -
  docker network inspect -f '{{.IPAM.Config}}' kind
  kubectl apply -f build/travis/test-configuration/metallb-config-"${INSTANCE}".yaml
done
rm kind-config-xsite.yaml
