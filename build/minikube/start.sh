#!/usr/bin/env bash

set -e -x

PROFILE=${1}

minikube profile ${PROFILE}
minikube start
