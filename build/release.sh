#!/usr/bin/env bash

set -e -x

DRY_RUN=${DRY_RUN:-true}


validate() {
  if [ -z "${RELEASE_NAME}" ]; then
     echo "Env variable RELEASE_NAME, which sets version to be released is unset or set to the empty string"
     exit 1
  fi
}


branch() {
  git branch -D Release_${RELEASE_NAME} || true
  git checkout -b Release_${RELEASE_NAME}
}


replace() {
  sed -i'.backup' "s/latest/${RELEASE_NAME}/g" deploy/operator.yaml
}


commit() {
  git commit -a -m "${RELEASE_NAME} release"
}


tag() {
  git tag "${RELEASE_NAME}"
}


push() {
  git push --tags origin
}


cleanup() {
  git checkout master
  git branch -D Release_${RELEASE_NAME} || true
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
