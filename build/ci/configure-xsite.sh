#!/usr/bin/env bash

. build/common.sh

METALLB_ADDRESS_SHIFT=25
METALLB_ADDRESS_START=200
export MAKE_DATADIR_WRITABLE=true
export GO111MODULE=on
export INITCONTAINER_IMAGE=registry.access.redhat.com/ubi8-micro
export TESTING_NAMESPACE=${TESTING_NAMESPACE-namespace-for-testing}
export KUBECONFIG=${KUBECONFIG-~/kind-kube-config.yaml}
KIND_KUBEAPI_PORT=6443
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

for INSTANCE_IDX in 1 2; do
  INSTANCE="xsite"${INSTANCE_IDX}
  export TESTING_NAMESPACE_XSITE="${TESTING_NAMESPACE}"-"${INSTANCE}"
  export KIND_INSTANCE="kind-"${INSTANCE}

  kind create cluster --config kind-config-xsite.yaml --name "${INSTANCE}"
  kind load docker-image infinispan-operator --name "${INSTANCE}"

  ./build/install-crds.sh kubectl
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

  # Configuring MetalLB (https://metallb.universe.tf/) for real LoadBalancer setup on Kind
  KIND_SUBNET=$(docker network inspect -f '{{ (index .IPAM.Config 0).Subnet }}' kind | grep -E -o "([0-9]{1,3}[\.]){3}[0-9]{1,3}")
  echo "Using Kind Subnet '${KIND_SUBNET}' in MetalLB"
  KIND_SUBNET_BASE=$(echo "${KIND_SUBNET}" | sed -e "s/0.0/255./g")
  METALLB_ADDRESSES_RANGE="${KIND_SUBNET_BASE}"$(("${METALLB_ADDRESS_START}" + ("${INSTANCE_IDX}" - 1) * ("${METALLB_ADDRESS_SHIFT}" + 1)))-"${KIND_SUBNET_BASE}"$(("${METALLB_ADDRESS_START}" + "${METALLB_ADDRESS_SHIFT}" + ("${INSTANCE_IDX}" - 1) * "${METALLB_ADDRESS_SHIFT}"))
  echo "MetalLB addresses range ${METALLB_ADDRESSES_RANGE} for instance ${INSTANCE}"
  kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/"${METALLB_VERSION}"/manifests/namespace.yaml
  kubectl create secret generic -n metallb-system memberlist --from-literal=secretkey="$(openssl rand -base64 128)"
  wget -q -O - https://raw.githubusercontent.com/metallb/metallb/"${METALLB_VERSION}"/manifests/metallb.yaml | sed "s|image: metallb/|image: quay.io/metallb/|" | kubectl apply -f -
  sed -e "s/METALLB_ADDRESSES_RANGE/${METALLB_ADDRESSES_RANGE}/g" build/ci/metallb-config-xsite.yaml | kubectl apply -f -

  # Validating MetalLB provisioning
  kubectl wait --for condition=available deployment controller -n metallb-system --timeout 150s
  kubectl wait --for condition=ready pod -l component=speaker -n metallb-system --timeout 150s

  # Update Kubernetes API with Kind docker network address
  NODE_INTERNAL_IP=$(kubectl get node "${INSTANCE}"-control-plane -o jsonpath="{.status.addresses[?(@.type=='InternalIP')].address}")
  export KIND_SERVER_KUBEAPI_URL="    server: https://"${NODE_INTERNAL_IP}":"${KIND_KUBEAPI_PORT}
  SERVER_URL=$(kind get kubeconfig --name ${INSTANCE} | grep "https://127.0.0.1")
  sed -i "s,${SERVER_URL},${KIND_SERVER_KUBEAPI_URL},g" "${KUBECONFIG}"
done
