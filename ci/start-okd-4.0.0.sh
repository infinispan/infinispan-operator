#!/usr/bin/env bash

docker run --name oc-cluster-up -d -v /etc/docker/certs.d/:/certs/ -v /tmp/:/tmp/ -v /var/run/docker.sock:/var/run/docker.sock gustavonalle/oc-cluster-up

function get_master {
   echo $(docker exec openshift-master oc describe node/openshift-master-node  | grep InternalIP | awk '{print $2}')
}

SLEEP=3
MAX_WAIT=100

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

download_oc_client

echo "Waiting for OKD to be ready..."

while ! curl -k -s "https://$(get_master):8443/healthz/ready"
do
    echo "Still waiting..."
    ((c++)) && ((c==$MAX_WAIT)) && exit 1
    sleep $SLEEP
done

