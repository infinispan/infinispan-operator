#!/usr/bin/env bash

set -e -x

oc apply -f deploy/10-catalogsource.cr.yaml
oc apply -f deploy/20-subscription.cr.yaml
oc apply -f deploy/30-operatorgroup.cr.yaml
