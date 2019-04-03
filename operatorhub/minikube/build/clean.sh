#!/usr/bin/env bash

set -e -x

kubectl delete ns operators || true
kubectl delete ns olm || true
kubectl delete clusterrole system:controller:operator-lifecycle-manager || true
kubectl delete clusterrole olm-operators-edit || true
kubectl delete clusterrole olm-operators-view || true
kubectl delete clusterrolebinding olm-operator-binding-olm || true
kubectl delete crd clusterserviceversions.operators.coreos.com || true
kubectl delete crd installplans.operators.coreos.com || true
kubectl delete crd subscriptions.operators.coreos.com || true
kubectl delete crd catalogsources.operators.coreos.com || true
kubectl delete crd operatorgroups.operators.coreos.com || true
