#!/usr/bin/env bash

. $(dirname "$0")/common.sh

YQ_VERSION=${1-4.6.1}

OPERATOR_ROLES_FILES=(
    "deploy/role.yaml"
    "deploy/clusterrole.yaml"
)

OPERATOR_CSV_FILE="deploy/olm-catalog/infinispan-operator.clusterserviceversion.yaml"

#Ensure that GOPATH/bin is in the PATH
if ! validateYQ; then
  printf "Valid yq not found in PATH. Trying adding GOPATH/bin\n"
  PATH=$(go env GOPATH)/bin:$PATH
  if ! validateYQ; then
    installYQ
  fi
fi

yq ea -i 'select(fileIndex == 0).spec.install.spec.permissions[0].rules=select(fileIndex == 1).rules|select(fileIndex == 0).spec.install.spec.clusterPermissions[0].rules=select(fileIndex == 2).rules|select(fileIndex==0)' "${OPERATOR_CSV_FILE}" "${OPERATOR_ROLES_FILES[@]}"
