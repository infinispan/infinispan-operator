#!/usr/bin/env bash

YQ_MAJ_VERSION=${1:-4}
installYQ() {
    printf "Installing yq ..."
    GO111MODULE=off go get -u github.com/myitcv/gobin
    gobin github.com/mikefarah/yq/v4@v4.6.1
    yq --version
}

OPERATOR_ROLES_FILES=(
    "deploy/role.yaml"
    "deploy/clusterrole.yaml"
)

OPERATOR_CSV_FILE="deploy/olm-catalog/infinispan-operator.clusterserviceversion.yaml"

validateYQ() {
 printf "Validating yq installation..."
if ! [ -x "$(command -v yq)" ]; then
  printf "Not found\n"
  return 1
else
  printf "OK\n"
  printf "Validating yq major version..."
  INSTALLED_YQ_MAJ_VERSION=$(yq --version | grep -oE 'yq version [0-9]+' | grep -oE '[0-9]+')
  if [ "${INSTALLED_YQ_MAJ_VERSION}" == "${YQ_MAJ_VERSION}" ]; then
    printf "%s\n" "${YQ_MAJ_VERSION}"
  else
    printf "incorrect major version %s\n" "${INSTALLED_YQ_MAJ_VERSION}"
    return 1
  fi
fi
 
}

#Ensure that GOPATH/bin is in the PATH
if ! validateYQ; then
  printf "Valid yq not found in PATH. Trying adding GOPATH/bin\n"
  PATH=$(go env GOPATH)/bin:$PATH
  if ! validateYQ; then
    installYQ
  fi
fi

yq ea -i 'select(fileIndex == 0).spec.install.spec.permissions[0].rules=select(fileIndex == 1).rules|select(fileIndex == 0).spec.install.spec.clusterPermissions[0].rules=select(fileIndex == 2).rules|select(fileIndex==0)' ${OPERATOR_CSV_FILE} ${OPERATOR_ROLES_FILES[@]}
