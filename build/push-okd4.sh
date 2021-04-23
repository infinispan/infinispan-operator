#!/usr/bin/env bash

. $(dirname "$0")/common.sh

YQ_VERSION=${1-4.6.1}

validateOC

if ! validateYQ; then
  printf "Valid yq not found in PATH. Trying adding GOPATH/bin\n"
  PATH=$(go env GOPATH)/bin:$PATH
  if ! validateYQ; then
    installYQ
  fi
fi

DOCKER_CERTS_FOLDER="/etc/docker/certs.d/"
REGISTRY_ROUTE=$(oc get route default-route -n openshift-image-registry -o json | jq -r '.spec.host')
OC_PROJECT_NAME=$(oc project -q)
PROJECT_NAME=${PROJECT_NAME-$OC_PROJECT_NAME}
export VERSION=${RELEASE_NAME-$(git describe --tags --always --dirty)}

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
yq ea -e '.metadata.labels["xtf.cz/keep"]="true"' deploy/role.yaml | oc apply -f -
yq ea -e '.metadata.labels["xtf.cz/keep"]="true"' deploy/service_account.yaml | oc apply -f -
yq ea -e '.metadata.labels["xtf.cz/keep"]="true"' deploy/role_binding.yaml | oc apply -f -
yq ea -e '.metadata.labels["xtf.cz/keep"]="true"' deploy/clusterrole.yaml | oc apply -f -
./build/install-crds.sh
yq ea -e '.subjects[0].namespace = strenv(PROJECT_NAME) | .metadata.labels["xtf.cz/keep"]="true"' deploy/clusterrole_binding.yaml | oc apply -f -
yq ea -e '.spec.template.spec.containers[0].image = "image-registry.openshift-image-registry.svc:5000/" + strenv(PROJECT_NAME) + "/infinispan-operator:" + strenv(VERSION) | .spec.template.metadata.labels["xtf.cz/keep"]="true" | .metadata.labels["xtf.cz/keep"]="true"' deploy/operator.yaml | oc apply -f -

oc patch is infinispan-operator --type=json -p '[{"op":"add","path":"/metadata/labels","value":{"xtf.cz/keep":"true"}}]'
