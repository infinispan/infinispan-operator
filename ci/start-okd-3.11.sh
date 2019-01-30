#!/usr/bin/env bash

set -x
set -e

OSE_MAIN_VERSION=v3.11.0
OSE_SHA1_VERSION=0cbc58b

function download_oc_client {
  echo "==== Installing OC Client ===="
  if [[ -f ./oc ]]; then
    echo "oc client installed"
  else
    wget -q -N https://github.com/openshift/origin/releases/download/$OSE_MAIN_VERSION/openshift-origin-client-tools-$OSE_MAIN_VERSION-$OSE_SHA1_VERSION-linux-64bit.tar.gz
    tar -zxf openshift-origin-client-tools-$OSE_MAIN_VERSION-$OSE_SHA1_VERSION-linux-64bit.tar.gz
    cp openshift-origin-client-tools-$OSE_MAIN_VERSION-$OSE_SHA1_VERSION-linux-64bit/oc .
    rm -rf openshift-origin-client-tools-$OSE_MAIN_VERSION+$OSE_SHA1_VERSION-linux-64bit
    rm -rf openshift-origin-client-tools-$OSE_MAIN_VERSION-$OSE_SHA1_VERSION-linux-64bit.tar.gz
  fi
}

function start_cluster {
  echo "==== Starting up cluster ===="
  ./oc cluster up

}

download_oc_client
start_cluster
