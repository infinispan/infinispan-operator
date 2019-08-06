#!/usr/bin/env bash

set -e -x

DRY_RUN=${DRY_RUN:-true}
CURRENT_BRANCH=$(git branch | grep \* | cut -d ' ' -f2)
BUILD_MANIFESTS_DIR=build/_output/olm-catalog
CSV_FILE="infinispan-operator.clusterserviceversion.yaml"


validate() {
  if [ -z "${RELEASE_NAME}" ]; then
     echo "Env variable RELEASE_NAME, which sets version to be released is unset or set to the empty string"
     exit 1
  fi

  if [ -z "${REPLACES_RELEASE_NAME}" ]; then
     echo "Env variable REPLACES_RELEASE_NAME, which sets version being replaced is unset or set to the empty string"
     exit 1
  fi

  if [ -z "${SERVER_VERSION}" ]; then
     echo "Env variable SERVER_VERSION, which sets server version to be use is unset or set to the empty string"
     exit 1
  fi

  if [ -z "${GITHUB_USERNAME}" ]; then
     echo "Env variable GITHUB_USERNAME, which sets the Git Hub username is unset or set to the empty string"
     exit 1
  fi
}


branch() {
  git branch -D Release_${RELEASE_NAME} || true
  git checkout -b Release_${RELEASE_NAME}
}


replace() {
  sed -i'.backup' "s/infinispan-server:latest/infinispan-server:${SERVER_VERSION}/g" deploy/operator.yaml
  sed -i'.backup' "s/infinispan-operator:latest/infinispan-operator:${RELEASE_NAME}/g" deploy/operator.yaml

  updateCsvFile deploy/olm-catalog
  updatePackageFile deploy/olm-catalog/infinispan.package.yaml
}


commit() {
  git commit -a -m "${RELEASE_NAME} release"
}


tag() {
  git tag -d "${RELEASE_NAME}" || echo "Tag does not exist"
  git tag "${RELEASE_NAME}"
}


push() {
  git push --tags origin
}


cleanup() {
  git checkout ${CURRENT_BRANCH}
  git branch -D Release_${RELEASE_NAME} || true
  rm -f deploy/*.backup
}


#manifests() {
#  local csvFileName="infinispan-operator.clusterserviceversion.yaml"
#  local releaseCsvFileName="infinispan-operator.v${RELEASE_NAME}.clusterserviceversion.yaml"
#  local releaseCsvFilePath=${BUILD_MANIFESTS_DIR}/${releaseCsvFileName}
#  local releasePackageFilePath=${BUILD_MANIFESTS_DIR}/infinispan.package.yaml
#
#  cp deploy/olm-catalog/* ${BUILD_MANIFESTS_DIR}
#  mv ${BUILD_MANIFESTS_DIR}/${csvFileName} ${releaseCsvFilePath}
#
#  sed -i'.backup' "s/9.9.9/${RELEASE_NAME}/g" ${releaseCsvFilePath}
#  sed -i'.backup' "s/9.9.8/${REPLACES_RELEASE_NAME}/g" ${releaseCsvFilePath}
#  sed -i'.backup' "s/infinispan-server:latest/infinispan-server:${SERVER_VERSION}/g" ${releaseCsvFilePath}
#  sed -i'.backup' "s/infinispan-operator:latest/infinispan-operator:${RELEASE_NAME}/g" ${releaseCsvFilePath}
#
#  local now="$(date +"%Y-%m-%dT%H:%M:%SZ")"
#  sed -i'.backup' "s/2000-01-01T12:00:00Z/${now}/g" ${releaseCsvFilePath}
#
#  sed -i'.backup' "s/9.9.9/${RELEASE_NAME}/g" ${releasePackageFilePath}
#}


operatorhub() {
  local repoDir=build/community-operators
  local upstreamDir=upstream-community-operators/infinispan
  local communityDir=community-operators/infinispan
  local upstreamBranch="infinispan_upstream_${RELEASE_NAME}"
  local communityBranch="infinispan_community_${RELEASE_NAME}"

  git clone git@github.com:${GITHUB_USERNAME}/community-operators.git ${repoDir}
  prepareBranches ${repoDir} ${upstreamBranch} ${upstreamDir}
  prepareBranches ${repoDir} ${communityBranch} ${communityDir}
}


prepareBranches() {
  local repoDir=$1
  local branch=$2
  local dir=$3

  local csvPath=${repoDir}/${dir}/${csvFile}
  local packageFile="infinispan.package.yaml"
  local packagePath=${repoDir}/${dir}/${packageFile}

  git checkout -b ${branch}
  cp deploy/olm-catalog/${csvFile} ${csvPath}
  cp deploy/olm-catalog/${packageFile} ${packagePath}

  pushd ${repoDir}
  updateCsvFile ${dir} ${csvFile}
  git commit -a -m "Copy Infinispan manifests for ${RELEASE_NAME} release"
  popd

  updatePackageFile ${packagePath}
  pushd ${repoDir}
  git commit -a -m "Update Infinispan manifests for ${RELEASE_NAME} release"
  popd
}

updateCsvFile() {
  local dir=$1

  local newName="infinispan-operator.v${RELEASE_NAME}.clusterserviceversion.yaml"
  local path=${dir}/${newName}

  mv ${dir}/${CSV_FILE} ${path}

  sed -i'.backup' "s/9.9.9/${RELEASE_NAME}/g" ${path}
  sed -i'.backup' "s/9.9.8/${REPLACES_RELEASE_NAME}/g" ${path}
  sed -i'.backup' "s/infinispan-server:latest/infinispan-server:${SERVER_VERSION}/g" ${path}
  sed -i'.backup' "s/infinispan-operator:latest/infinispan-operator:${RELEASE_NAME}/g" ${path}

  local now="$(date +"%Y-%m-%dT%H:%M:%SZ")"
  sed -i'.backup' "s/2000-01-01T12:00:00Z/${now}/g" ${path}

  git add ${path}
}


updatePackageFile() {
  local path=$1
  sed -i'.backup' "s/9.9.9/${RELEASE_NAME}/g" ${path}
}


main() {
  validate
  branch
  replace
  commit
  tag
  cleanup

  if [[ "${DRY_RUN}" = true ]] ; then
    echo "DRY_RUN is set to true. Skipping..."
  else
    push
  fi
}


main
