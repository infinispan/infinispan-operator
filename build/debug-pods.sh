#!/usr/bin/env bash

TESTING_NAMESPACE=${TESTING_NAMESPACE-namespace-for-testing}

while :
do
	echo "Looking for Stateful Sets to be running"
	SSS=$(kubectl get statefulsets -o name -n "${TESTING_NAMESPACE}")
	for SS in "${SSS[@]}"; do
	  if [ -n "${SS}" ]; then
	    echo "${SS}" "is ready for logs"
	    kubectl get pods -o yaml >> debug.log
	    kubectl get infinispan -o yaml >> debug.log
	    kubectl get statefulsets -o yaml >> debug.log
	    kubectl logs -f "${SS}" -n "${TESTING_NAMESPACE}" >> debug.log
	  fi
	done
	sleep 5
done
