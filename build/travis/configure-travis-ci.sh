#!/usr/bin/env bash

export MAKE_DATADIR_WRITABLE=true
export GO111MODULE=on
export RUN_SA_OPERATOR=TRUE
export INITCONTAINER_IMAGE=quay.io/quay/busybox:latest
export KUBECONFIG=~/kind-kube-config.yaml

curl -LO https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl && chmod +x kubectl && sudo mv kubectl /usr/local/bin/
curl -Lo kind https://kind.sigs.k8s.io/dl/"${KIND_VERSION}"/kind-linux-amd64 && chmod +x kind && sudo mv kind /usr/local/bin/
kind create cluster --config kind-config.yaml
kind get kubeconfig > "${KUBECONFIG}"
export TESTING_CONTEXT=$(kubectl --insecure-skip-tls-verify config current-context)
./build/travis/prepare-sa-test.sh
