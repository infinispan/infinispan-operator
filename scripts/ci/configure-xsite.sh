#!/usr/bin/env bash

IMG=infinispan-operator
METALLB_ADDRESS_SHIFT=25
METALLB_ADDRESS_START=200
KUBECONFIG=${KUBECONFIG-~/kind-kube-config.yaml}
KIND_KUBEAPI_PORT=6443
KIND_VERSION=v0.11.0
METALLB_VERSION=v0.9.6
TESTING_NAMESPACE=${TESTING_NAMESPACE-namespace-for-testing}

# Cleanup any existing clusters
kind delete clusters --all

# Common part for both nodes
make operator-build IMG=$IMG

for INSTANCE_IDX in 1 2; do
  INSTANCE="xsite"${INSTANCE_IDX}

  kind create cluster --config kind-config-xsite.yaml --name "${INSTANCE}"
  kind load docker-image $IMG --name "${INSTANCE}"

  TESTING_NAMESPACE_XSITE="${TESTING_NAMESPACE}-${INSTANCE}"
  kubectl create namespace "${TESTING_NAMESPACE_XSITE}"

  make deploy IMG=$IMG DEPLOYMENT_NAMESPACE="${TESTING_NAMESPACE_XSITE}"

  # Set the imagePullPolicy to Never so the loaded image is used
  kubectl -n ${TESTING_NAMESPACE_XSITE} patch deployment infinispan-operator-controller-manager -p \
  '{"spec": {"template": {"spec":{"containers":[{"name":"manager","imagePullPolicy":"Never","env": [{"name": "TEST_ENVIRONMENT","value": "true"}]}]}}}}'

  # Creating service account for the cross cluster token based auth
  kubectl create clusterrole xsite-cluster-role --verb=get,list,watch --resource=nodes,services
  kubectl create clusterrolebinding xsite-cluster-role-binding --clusterrole=xsite-cluster-role --serviceaccount=infinispan-operator-controller-manager

  # Configuring MetalLB (https://metallb.universe.tf/) for real LoadBalancer setup on Kind
  KIND_SUBNET=$(docker network inspect -f '{{ (index .IPAM.Config 0).Subnet }}' kind | grep -E -o "([0-9]{1,3}[\.]){3}[0-9]{1,3}")
  echo "Using Kind Subnet '${KIND_SUBNET}' in MetalLB"
  KIND_SUBNET_BASE=$(echo "${KIND_SUBNET}" | sed -E 's|([0-9]+\.[0-9]+\.)[0-9]+\.[0-9]+|\1255.|')
  METALLB_ADDRESSES_RANGE="${KIND_SUBNET_BASE}"$(("${METALLB_ADDRESS_START}" + ("${INSTANCE_IDX}" - 1) * ("${METALLB_ADDRESS_SHIFT}" + 1)))-"${KIND_SUBNET_BASE}"$(("${METALLB_ADDRESS_START}" + "${METALLB_ADDRESS_SHIFT}" + ("${INSTANCE_IDX}" - 1) * "${METALLB_ADDRESS_SHIFT}"))
  echo "MetalLB addresses range ${METALLB_ADDRESSES_RANGE} for instance ${INSTANCE}"
  kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/"${METALLB_VERSION}"/manifests/namespace.yaml
  kubectl create secret generic -n metallb-system memberlist --from-literal=secretkey="$(openssl rand -base64 128)"
  wget -q -O - https://raw.githubusercontent.com/metallb/metallb/"${METALLB_VERSION}"/manifests/metallb.yaml | sed "s|image: metallb/|image: quay.io/metallb/|" | kubectl apply -f -
  sed -e "s/METALLB_ADDRESSES_RANGE/${METALLB_ADDRESSES_RANGE}/g" scripts/ci/metallb-config-xsite.yaml | kubectl apply -f -

  # Validating MetalLB provisioning
  kubectl wait --for condition=available deployment controller -n metallb-system --timeout 150s
  kubectl wait --for condition=ready pod -l component=speaker -n metallb-system --timeout 150s

  # Update Kubernetes API with Kind docker network address
  NODE_INTERNAL_IP=$(kubectl get node "${INSTANCE}"-control-plane -o jsonpath="{.status.addresses[?(@.type=='InternalIP')].address}")
  export KIND_SERVER_KUBEAPI_URL="    server: https://"${NODE_INTERNAL_IP}":"${KIND_KUBEAPI_PORT}
  SERVER_URL=$(kind get kubeconfig --name ${INSTANCE} | grep "https://127.0.0.1")
  sed -i "s,${SERVER_URL},${KIND_SERVER_KUBEAPI_URL},g" "${KUBECONFIG}"
done
