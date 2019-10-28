#!/usr/bin/env bash

pushd ../..
minikube profile "$1";
eval "$(minikube docker-env)"
echo "${DOCKER_HOST}"
docker build -t jboss/infinispan-operator:latest . -f build/Dockerfile.single
