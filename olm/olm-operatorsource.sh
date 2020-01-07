cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1
kind: OperatorSource
metadata:
  name: ${QUAY_USERNAME}-operators
  namespace: openshift-marketplace
spec:
  type: appregistry
  endpoint: https://quay.io/cnr
  registryNamespace: ${QUAY_USERNAME}
  displayName: "${QUAY_USERNAME}'s Operators"
  publisher: "${QUAY_USERNAME}"
EOF
