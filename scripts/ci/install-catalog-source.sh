#!/usr/bin/env bash
set -e

KUBECONFIG=${KUBECONFIG-~/kind-kube-config.yaml}
TESTING_NAMESPACE=${TESTING_NAMESPACE-namespace-for-testing}
IMG_REGISTRY=${IMG_REGISTRY-"localhost:5000"}

export CHANNELS=2.3.x
export DEFAULT_CHANNEL=2.3.x
export VERSION=2.3.3

BUNDLE_IMG_NAME=infinispan-operator-bundle

export IMG=${IMG_REGISTRY}/infinispan-operator
export BUNDLE_IMG=${IMG_REGISTRY}/${BUNDLE_IMG_NAME}:v${VERSION}
export CATALOG_IMG=${IMG_REGISTRY}/infinispan-test-catalog
export CATALOG_BASE_IMG=${CATALOG_BASE_IMG-"quay.io/operatorhubio/catalog:latest"}

# Create the operator image
make operator-build operator-push

# Create the operator bundle image
make bundle bundle-build bundle-push

# Create the OLM catalog image
make catalog-build catalog-push

# Create the namespace and CatalogSource
kubectl create namespace ${TESTING_NAMESPACE} || true
kubectl delete CatalogSource test-catalog -n ${TESTING_NAMESPACE} || true
cat <<EOF | kubectl apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: test-catalog
  namespace: ${TESTING_NAMESPACE}
spec:
  displayName: Test Operators Catalog
  image: ${CATALOG_IMG}
  sourceType: grpc
EOF
