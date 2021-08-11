#!/usr/bin/env bash

. $(dirname "$0")/common.sh

installRice() {
  printf "Installing rice ..."
  installGoBin
  gobin github.com/GeertJohan/go.rice/rice
}

validateRice() {
  printf "Validating rice installation..."
  if ! [ -x "$(command -v rice)" ]; then
    printf "Not found\n"
    return 1
  else
    printf "OK\n"
  fi
}

#Ensure that GOPATH/bin is in the PATH
if ! validateRice; then
  printf "Valid rice not found in PATH. Trying adding GOPATH/bin\n"
  PATH=$(go env GOPATH)/bin:$PATH
  if ! validateRice; then
    installRice
  fi
fi

rice embed-go -i pkg/controller/infinispan/resources/operatorconfig/grafana.go
rice embed-go -i controllers/dependencies.go
sed -i "s|time.Unix(.*, 0)|time.Unix(1620137619, 0)|" pkg/controller/infinispan/resources/operatorconfig/rice-box.go
sed -i "s|time.Unix(.*, 0)|time.Unix(1620137619, 0)|" pkg/controllers/rice-box.go
