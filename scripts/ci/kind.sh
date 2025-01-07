#!/usr/bin/env bash
# Modified version of the script found at https://kind.sigs.k8s.io/docs/user/local-registry/#create-a-cluster-and-registry
set -o errexit

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
source "${SCRIPT_DIR}/operand_common.sh"

if ! command -v docker &> /dev/null; then
    echo "docker not installed, trying podman"
    function docker() {
        podman "$@"
    }
fi

if [[ -z "${SERVER_IMAGES}" ]]; then
  SERVER_IMAGES=$(operandJson | jq -r '.[].image')
fi
DOCKER_REGISTRY_IMAGE=${DOCKER_REGISTRY_IMAGE:-"quay.io/infinispan-test/registry:2"}
KINDEST_IMAGE=${KINDEST_IMAGE:-"quay.io/infinispan-test/kindest-node"}
KINDEST_NODE_VERSION=${KINDEST_NODE_VERSION:-'v1.25.16'}
KIND_SUBNET=${KIND_SUBNET-172.172.0.0}

docker network create kind --subnet "${KIND_SUBNET}/16" || true

# create registry container unless it already exists
reg_name='kind-registry'
reg_port=${KIND_PORT-'5001'}
running="$(docker inspect -f '{{.State.Running}}' "${reg_name}" 2>/dev/null || true)"
if [ "${running}" != 'true' ]; then
  docker run \
    -d --restart=always -p "127.0.0.1:${reg_port}:5000" --name "${reg_name}" \
    ${DOCKER_REGISTRY_IMAGE}
fi

# create a cluster with the local registry enabled in containerd
cat <<EOF | kind create cluster --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:${reg_port}"]
    endpoint = ["http://${reg_name}:5000"]
nodes:
  - role: control-plane
    image: ${KINDEST_IMAGE}:${KINDEST_NODE_VERSION}
    extraPortMappings:
      - containerPort: 30222
        hostPort: 11222
EOF

# connect the registry to the cluster network
# (the network may already be connected)
docker network connect "kind" "${reg_name}" || true

# Attempt to load the servers image to prevent them being pulled again
for img in ${SERVER_IMAGES}; do
  kind load docker-image "${img}" || true
done

# Document the local registry
# https://github.com/kubernetes/enhancements/tree/master/keps/sig-cluster-lifecycle/generic/1755-communicating-a-local-registry
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${reg_port}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF
