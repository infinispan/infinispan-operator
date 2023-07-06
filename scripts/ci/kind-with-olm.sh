#!/usr/bin/env bash
set -o errexit

# Wait for k8s resource to exist. See: https://github.com/kubernetes/kubernetes/issues/83242
waitFor() {
  xtrace=$(set +o|grep xtrace); set +x
  local ns=${1?namespace is required}; shift
  local type=${1?type is required}; shift

  echo "Waiting for $type $*"
  until oc -n "$ns" get "$type" "$@" -o=jsonpath='{.items[0].metadata.name}' >/dev/null 2>&1; do
    echo "Waiting for $type $*"
    sleep 1
  done
  eval "$xtrace"
}

SCRIPT_DIR=$(dirname "$0")

OLM_VERSION="v0.21.2"

source $SCRIPT_DIR/kind.sh

operator-sdk olm install --version ${OLM_VERSION}

# Sometimes olm install does not wait long enough for deployments to be rolled out
kubectl wait --for=condition=available --timeout=60s deployment/catalog-operator -n olm
kubectl wait --for=condition=available --timeout=60s deployment/olm-operator -n olm
kubectl wait --for=condition=available --timeout=60s deployment/packageserver -n olm

# Install Cryostat Operator
cat <<EOF | kubectl apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: cryostat
  namespace: operators
spec:
  channel: stable
  name: cryostat-operator
  source: operatorhubio-catalog
  sourceNamespace: olm
EOF

waitFor operators deployment cryostat-operator-controller-manager
kubectl wait --for=condition=available --timeout=60s deployment.apps/cryostat-operator-controller-manager -n operators
