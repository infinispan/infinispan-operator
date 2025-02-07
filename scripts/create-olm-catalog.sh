#!/usr/bin/env bash
set -e

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
CATALOG_DIR=infinispan-olm-catalog
DOCKERFILE=${CATALOG_DIR}.Dockerfile
CATALOG=${CATALOG_DIR}/catalog.yaml

BUNDLE_IMGS="${BUNDLE_IMG}"
# Define existing bundle images required in the catalog
for version in v2.3.0 v2.3.1 v2.3.2 v2.3.3 v2.3.4 v2.3.5 v2.3.6 v2.3.7 v2.4.0 v2.4.1 v2.4.2 v2.4.3 v2.4.4 v2.4.5 v2.4.6 v2.4.7 v2.4.8 v2.4.9 v2.4.10; do
  BUNDLE_IMGS="${BUNDLE_IMGS} quay.io/operatorhubio/infinispan:$version"
done

rm -rf ${CATALOG_DIR}
mkdir ${CATALOG_DIR}

cp ${SCRIPT_DIR}/test-catalog.yml ${CATALOG}
${OPM} render --use-http -o yaml ${BUNDLE_IMGS} >> ${CATALOG}

${OPM} validate ${CATALOG_DIR}
${OPM} generate dockerfile ${CATALOG_DIR}
${CONTAINER_TOOL} build -f ${DOCKERFILE} -t ${CATALOG_IMG} .

rm -rf ${DOCKERFILE}
