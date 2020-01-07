OPERATOR_NAMESPACE=$1
PACKAGES=$2
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1
kind: CatalogSourceConfig
metadata:
  name: infinispan
  namespace: openshift-marketplace
spec:
  targetNamespace: ${OPERATOR_NAMESPACE}
  packages: ${PACKAGES}
  source: ${QUAY_USERNAME}-operators
EOF
