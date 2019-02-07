#!/usr/bin/env bash

docker run --name oc-cluster-up -d -v /etc/docker/certs.d/:/certs/ -v /tmp/:/tmp/ -v /var/run/docker.sock:/var/run/docker.sock gustavonalle/oc-cluster-up

function get_master {
   echo $(docker exec openshift-master oc describe node/openshift-master-node  | grep InternalIP | awk '{print $2}')
}

SLEEP=3
MAX_WAIT=100

echo "Waiting for OKD to be ready..."

while ! curl -k -s "https://$(get_master):8443/healthz/ready"
do
    echo "Still waiting..."
    ((c++)) && ((c==$MAX_WAIT)) && exit 1
    sleep $SLEEP
done
