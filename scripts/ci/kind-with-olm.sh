#!/usr/bin/env bash
set -o errexit

SCRIPT_DIR=$(dirname "$0")

OLM_VERSION="v0.28.0"

source $SCRIPT_DIR/kind.sh

operator-sdk olm install --version ${OLM_VERSION}

# Sometimes olm install does not wait long enough for deployments to be rolled out
kubectl wait --for=condition=available --timeout=60s deployment/catalog-operator -n olm
kubectl wait --for=condition=available --timeout=60s deployment/olm-operator -n olm
kubectl wait --for=condition=available --timeout=60s deployment/packageserver -n olm
