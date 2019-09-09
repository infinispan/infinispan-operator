#!/usr/bin/env bash

set -e -x

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

build() {
docker build -t releaser -f build/jenkins/Dockerfile .
eval "$(ssh-agent -s)" && ssh-add "${keyfile}" && docker run --privileged=true -v ${SSH_AUTH_SOCK}:/ssh-agent -e SSH_AUTH_SOCK=/ssh-agent releaser \
	bash -c "(mkdir ~/.ssh && ssh-keyscan -H github.com >> ~/.ssh/known_hosts) && git config --global user.email "infinispan@infinispan.org" && git config --global user.name "Infinispan" &&  make release RELEASE_NAME=${RELEASE_NAME} SERVER_VERSION=${SERVER_VERSION} KEEP_BRANCH=true REPLACES_RELEASE_NAME=${REPLACES_RELEASE_NAME} GITHUB_USERNAME=${GITHUB_USERNAME} DRY_RUN=${DRY_RUN} NO_PR=${NO_PR}"
}

main() {
	validate
	build
}

main
