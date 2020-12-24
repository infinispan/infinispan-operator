#!/usr/bin/env bash

oc apply -f deploy/crds/infinispan.org_infinispans_crd.yaml
oc apply -f deploy/crds/infinispan.org_caches_crd.yaml
