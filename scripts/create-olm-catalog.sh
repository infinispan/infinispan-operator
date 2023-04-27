#!/usr/bin/env bash
set -e

CATALOG_DIR=infinispan-olm-catalog
DOCKERFILE=${CATALOG_DIR}.Dockerfile
CATALOG=${CATALOG_DIR}/catalog.yaml

BUNDLE_IMGS="${BUNDLE_IMG}"
# Define existing bundle images required in the catalog
for version in v2.2.5 v2.3.0 v2.3.1 v2.3.2; do
  BUNDLE_IMGS="${BUNDLE_IMGS} quay.io/operatorhubio/infinispan:$version"
done

rm -rf ${CATALOG_DIR}
mkdir ${CATALOG_DIR}

# Define OLM update graph
cat <<EOF >> ${CATALOG}
---
schema: olm.package
name: infinispan
defaultChannel: 2.3.x
---
schema: olm.channel
name: 2.3.x
package: infinispan
entries:
- name: infinispan-operator.v2.3.3
  replaces: infinispan-operator.v2.3.2
- name: infinispan-operator.v2.3.2
  replaces: infinispan-operator.v2.3.1
- name: infinispan-operator.v2.3.1
  replaces: infinispan-operator.v2.3.0
- name: infinispan-operator.v2.3.0
  replaces: infinispan-operator.v2.2.5
- name: infinispan-operator.v2.2.5
  replaces: infinispan-operator.v2.2.4
---
schema: olm.channel
name: 2.2.x
package: infinispan
entries:
- name: infinispan-operator.v2.2.5
  replaces: infinispan-operator.v2.2.4
EOF

${OPM} render --use-http -o yaml ${BUNDLE_IMGS} >> ${CATALOG}

${OPM} validate ${CATALOG_DIR}
${OPM} generate dockerfile ${CATALOG_DIR}
${CONTAINER_TOOL} build -f ${DOCKERFILE} -t ${CATALOG_IMG} .

rm -rf ${DOCKERFILE}
