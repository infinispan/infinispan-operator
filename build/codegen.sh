#!/usr/bin/env bash

rm -rf pkg/generated

vendor/k8s.io/code-generator/generate-groups.sh \
all  \
github.com/infinispan/infinispan-operator/pkg/generated \
github.com/infinispan/infinispan-operator/pkg/apis \
infinispan:v1
