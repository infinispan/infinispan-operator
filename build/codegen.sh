#!/usr/bin/env bash

location=$(dirname $0)
rootdir=$location/..

unset GOPATH
GO111MODULE=on

echo "Generating Go client code..."

go run k8s.io/code-generator/cmd/client-gen \
        --input=infinispan/v1 \
        --go-header-file=$rootdir/build/boilerplate.go.txt \
        --clientset-name="versioned"  \
        --input-base=github.com/infinispan/infinispan-operator/pkg/apis \
        --output-base=$rootdir \
        --output-package=github.com/infinispan/infinispan-operator/pkg/generated/clientset

go run k8s.io/code-generator/cmd/lister-gen \
        --input-dirs=github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1 \
        --go-header-file=$rootdir/build/boilerplate.go.txt \
        --output-base=$rootdir \
        --output-package=github.com/infinispan/infinispan-operator/pkg/generated/listers

go run k8s.io/code-generator/cmd/informer-gen \
        --versioned-clientset-package=github.com/infinispan/infinispan-operator/pkg/generated/clientset/versioned \
        --go-header-file=$rootdir/build/boilerplate.go.txt \
        --listers-package=github.com/infinispan/infinispan-operator/pkg/generated/listers \
        --input-dirs=github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1 \
        --output-base=$rootdir \
        --output-package=github.com/infinispan/infinispan-operator/pkg/generated/informers

# hack to fix non go-module compliance
cp -R $rootdir/github.com/infinispan/infinispan-operator/pkg $rootdir
rm -rf $rootdir/github.com
