#!/usr/bin/env bash

set -e
set -x

KUBECONFIG=${1-openshift.local.clusterup/openshift-apiserver/admin.kubeconfig}


main() {
  echo "Using KUBECONFIG '$KUBECONFIG'"

  local targetDir="/tmp/openshift-dind-cluster/openshift/openshift.local.config/master"
  local containerId=`docker ps -aqf "ancestor=gustavonalle/oc-cluster-up" | sed 1q`

  mkdir -p ${targetDir}
  docker cp ${containerId}:${targetDir}/admin.kubeconfig $KUBECONFIG
}


main
