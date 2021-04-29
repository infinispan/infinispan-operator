#!/usr/bin/env bash

KUBECTL_RUNTIME=${1-oc}

"${KUBECTL_RUNTIME}" apply -f deploy/crds/infinispan.org_infinispans_crd.yaml
"${KUBECTL_RUNTIME}" apply -f deploy/crds/infinispan.org_caches_crd.yaml
"${KUBECTL_RUNTIME}" apply -f deploy/crds/infinispan.org_backups_crd.yaml
"${KUBECTL_RUNTIME}" apply -f deploy/crds/infinispan.org_restores_crd.yaml
"${KUBECTL_RUNTIME}" apply -f deploy/crds/infinispan.org_batches_crd.yaml
