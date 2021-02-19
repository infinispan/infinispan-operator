#!/bin/bash
TESTING_NAMESPACE=${TESTING_NAMESPACE-namespace-for-testing}

kubectl delete namespace "$TESTING_NAMESPACE" --wait=true
kubectl create namespace "$TESTING_NAMESPACE"
kubectl apply -f deploy/service_account.yaml -n "$TESTING_NAMESPACE"

KUBE_DEPLOY_SECRET_NAME=$(kubectl -n "$TESTING_NAMESPACE" get sa infinispan-operator -o jsonpath='{.secrets[-1].name}')
KUBE_API_EP=$(kubectl get ep kubernetes -n default -o jsonpath='{.subsets[0].addresses[0].ip}')
KUBE_API_TOKEN=$(kubectl -n "$TESTING_NAMESPACE" get secret "$KUBE_DEPLOY_SECRET_NAME" -o jsonpath='{.data.token}'|base64 --decode)

cat deploy/role.yaml build/debug-role-patch.yaml | kubectl apply -n "$TESTING_NAMESPACE" -f -
kubectl apply -f deploy/role_binding.yaml -n "$TESTING_NAMESPACE"
kubectl apply -f deploy/clusterrole.yaml -n "$TESTING_NAMESPACE"
sed "s/namespace: infinispan-operator/namespace: $TESTING_NAMESPACE/" deploy/clusterrole_binding.yaml | kubectl apply -f -

kubectl --insecure-skip-tls-verify config set-cluster k8s --server=https://"$KUBE_API_EP":6443
kubectl --insecure-skip-tls-verify config set-credentials user-for-testing --token="$KUBE_API_TOKEN"
kubectl --insecure-skip-tls-verify config set-context k8s --cluster k8s --user user-for-testing --namespace "$TESTING_NAMESPACE"
kubectl --insecure-skip-tls-verify config use-context k8s

