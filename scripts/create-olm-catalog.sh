#!/usr/bin/env bash
set -e

CATALOG_DIR=infinispan-olm-catalog
DOCKERFILE=${CATALOG_DIR}.Dockerfile
CATALOG=${CATALOG_DIR}/catalog.json
BUNDLE=${CATALOG_DIR}/bundle.json

mkdir -p ${CATALOG_DIR}

${OPM} render --use-http ${CATALOG_BASE_IMG} > ${CATALOG}
${OPM} render --use-http ${BUNDLE_IMGS} > ${BUNDLE}

default_channel=$(${JQ} 'select((.name=="infinispan") and (.schema=="olm.package"))' ${CATALOG} | ${JQ} -r .defaultChannel)
channel_selector="(.package==\"infinispan\") and (.schema==\"olm.channel\") and (.name==\"${default_channel}\")"
old_channel=$(${JQ} -r "select(${channel_selector})" ${CATALOG})
old_channel_latest_bundle=$(echo $old_channel | ${JQ} -r '.entries[-1].name')

bundle_name=$(${JQ} -r .name ${BUNDLE})
new_channel=$(echo $old_channel | ${JQ} ".entries += [{\"name\":\"${bundle_name}\", \"replaces\":\"${old_channel_latest_bundle}\"}]")

new_catalog=$(${JQ} -r "select(${channel_selector} | not)" ${CATALOG})
echo ${new_channel} ${new_catalog} > ${CATALOG_DIR}/new_catalog.json

# Remove the original catalog no longer required
rm -f ${CATALOG}

${OPM} validate ${CATALOG_DIR}
${OPM} generate dockerfile ${CATALOG_DIR}
${CONTAINER_TOOL} build -f ${DOCKERFILE} -t ${CATALOG_IMG} .

rm -rf ${DOCKERFILE}
