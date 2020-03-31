#!/usr/bin/env bash

validateOC() {
  OC_USER_NAME=$(oc whoami 2>/dev/null)
  if [ ! $? -eq 0 ]; then
    echo "You must login with 'oc login' or set KUBECONFIG env variable"
    exit 1
  fi
}

validateOC

DOCKER_CERTS_FOLDER="/etc/docker/certs.d/"
REGISTRY_ROUTE=$(oc get route default-route -n openshift-image-registry -o json | jq -r '.spec.host')
OC_PROJECT_NAME=$(oc project -q)
PROJECT_NAME=${PROJECT_NAME-$OC_PROJECT_NAME}
VERSION=${RELEASE_NAME-$(git describe --tags --always --dirty)}

validateROOT() {
  if ! [ $(id -u) = 0 ]; then
    echo "It's necessary to run next command with sudo!"
    sudo ls >/dev/null
    if [ ! $? -eq 0 ]; then
      echo "Invalid sudo credentials"
      exit 1
    fi
  fi
}

if [ -d "${DOCKER_CERTS_FOLDER}${REGISTRY_ROUTE}" ]; then
  echo "Instance folder detected"
else
  echo "Instance folder not found, creating..."
  validateROOT
  sudo mkdir -p "${DOCKER_CERTS_FOLDER}${REGISTRY_ROUTE}"
fi

if [ -f "${DOCKER_CERTS_FOLDER}${REGISTRY_ROUTE}/tls.crt" ]; then
  echo "Instance file detected"
else
  echo "Instance file not found, extracting..."
  validateROOT
  oc extract secret/router-ca --keys=tls.crt -n openshift-ingress-operator
  sudo mv ./tls.crt "${DOCKER_CERTS_FOLDER}${REGISTRY_ROUTE}/"
fi

oc registry login --skip-check
docker build -t "${REGISTRY_ROUTE}/${PROJECT_NAME}/infinispan-operator:${VERSION}" . -f ./build/Dockerfile.single
docker push "${REGISTRY_ROUTE}/${PROJECT_NAME}/infinispan-operator:${VERSION}"

oc project "${PROJECT_NAME}"
oc apply -f deploy/role.yaml
oc apply -f deploy/service_account.yaml
oc apply -f deploy/role_binding.yaml
oc apply -f deploy/crds/infinispan.org_infinispans_crd.yaml
oc apply -f deploy/crds/infinispan.org_caches_crd.yaml
sed -e "s|jboss/infinispan-operator:latest|image-registry.openshift-image-registry.svc:5000/${PROJECT_NAME}/infinispan-operator:${VERSION}|" deploy/operator.yaml | oc apply -f -
