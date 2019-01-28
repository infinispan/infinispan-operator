#!/usr/bin/env bash

rm -rf pkg/generated

vendor/k8s.io/code-generator/generate-groups.sh \
all  \
github.com/jboss-dockerfiles/infinispan-server-operator/pkg/generated \
github.com/jboss-dockerfiles/infinispan-server-operator/pkg/apis \
infinispan:v1
