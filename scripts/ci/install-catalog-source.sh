#!/usr/bin/env bash
set -e

KUBECONFIG=${KUBECONFIG-~/kind-kube-config.yaml}
TESTING_NAMESPACE=${TESTING_NAMESPACE-namespace-for-testing}
IMG_REGISTRY=${IMG_REGISTRY-"localhost:5000"}

export CHANNELS=2.2.x
export DEFAULT_CHANNEL=2.2.x
export VERSION=2.2.4

BUNDLE_IMG_NAME=infinispan-operator-bundle

export IMG=${IMG_REGISTRY}/infinispan-operator
export BUNDLE_IMG=${IMG_REGISTRY}/${BUNDLE_IMG_NAME}:v${VERSION}
export CATALOG_IMG=${IMG_REGISTRY}/infinispan-test-catalog
export CATALOG_BASE_IMG=${CATALOG_BASE_IMG-"quay.io/operatorhubio/catalog_tmp"}

# Create the operator image
make operator-build operator-push

# Create the operator bundle image
# We must capture the sha256 digest of the image for use with catalog-build later
PUSH_OUTPUT=$(make bundle bundle-build bundle-push | tail -1)
BUNDLE_IMG_DIGEST=$(echo "${PUSH_OUTPUT}" | awk '/:/ {print $3}')

# Create the Catalog image
# It's necessary to reference the image using sha256 digest when calling `make catalog-build`
# If the sha isn't used, then the local tag is overwritten with a modified image that sets `bundle.channels.v1` to`alpha`
# This doesn't occur if using an image that is not hosted on localhost:5000, i.e. quay.io.
export BUNDLE_IMG=${IMG_REGISTRY}/${BUNDLE_IMG_NAME}@${BUNDLE_IMG_DIGEST}
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
