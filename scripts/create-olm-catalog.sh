#!/usr/bin/env bash
set -e

CATALOG_DIR=infinispan-olm-catalog
DOCKERFILE=${CATALOG_DIR}.Dockerfile
CATALOG=${CATALOG_DIR}/catalog.yaml

BUNDLE_IMGS="${BUNDLE_IMG}"
# Define existing bundle images required in the catalog
for version in v2.3.0 v2.3.1 v2.3.2 v2.3.3 v2.3.4 v2.3.5 v2.3.6 v2.3.7 v2.4.0 v2.4.1 v2.4.2 v2.4.3; do
  BUNDLE_IMGS="${BUNDLE_IMGS} quay.io/operatorhubio/infinispan:$version"
done

rm -rf ${CATALOG_DIR}
mkdir ${CATALOG_DIR}

# Define OLM update graph
cat <<EOF >> ${CATALOG}
---
schema: olm.package
name: infinispan
defaultChannel: stable
---
schema: olm.channel
name: stable
package: infinispan
entries:
  - name: infinispan-operator.v2.4.4
    replaces: infinispan-operator.v2.4.3
  - name: infinispan-operator.v2.4.3
    replaces: infinispan-operator.v2.4.2
  - name: infinispan-operator.v2.4.2
    replaces: infinispan-operator.v2.4.1
  - name: infinispan-operator.v2.4.1
    replaces: infinispan-operator.v2.4.0
  - name: infinispan-operator.v2.4.0
    replaces: infinispan-operator.v2.3.7
  - name: infinispan-operator.v2.3.7
    replaces: infinispan-operator.v2.3.6
  - name: infinispan-operator.v2.3.6
    replaces: infinispan-operator.v2.3.5
  - name: infinispan-operator.v2.3.5
    replaces: infinispan-operator.v2.3.4
  - name: infinispan-operator.v2.3.4
    replaces: infinispan-operator.v2.3.3
  - name: infinispan-operator.v2.3.3
    replaces: infinispan-operator.v2.3.2
  - name: infinispan-operator.v2.3.2
    replaces: infinispan-operator.v2.3.1
  - name: infinispan-operator.v2.3.1
    replaces: infinispan-operator.v2.3.0
  - name: infinispan-operator.v2.3.0
    replaces: infinispan-operator.v2.2.5
---
schema: olm.channel
name: 2.3.x
package: infinispan
entries:
- name: infinispan-operator.v2.3.7
  replaces: infinispan-operator.v2.3.6
- name: infinispan-operator.v2.3.6
  replaces: infinispan-operator.v2.3.5
- name: infinispan-operator.v2.3.5
  replaces: infinispan-operator.v2.3.4
- name: infinispan-operator.v2.3.4
  replaces: infinispan-operator.v2.3.3
- name: infinispan-operator.v2.3.3
  replaces: infinispan-operator.v2.3.2
- name: infinispan-operator.v2.3.2
  replaces: infinispan-operator.v2.3.1
- name: infinispan-operator.v2.3.1
  replaces: infinispan-operator.v2.3.0
- name: infinispan-operator.v2.3.0
  replaces: infinispan-operator.v2.2.5
EOF

${OPM} render --use-http -o yaml ${BUNDLE_IMGS} >> ${CATALOG}

${OPM} validate ${CATALOG_DIR}
${OPM} generate dockerfile ${CATALOG_DIR}
${CONTAINER_TOOL} build -f ${DOCKERFILE} -t ${CATALOG_IMG} .

rm -rf ${DOCKERFILE}
