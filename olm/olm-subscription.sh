OPERATOR_NAMESPACE=$1
PACKAGE_NAME=$2
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: infinispan
  namespace: ${OPERATOR_NAMESPACE}
spec:
  channel: dev-preview
  installPlanApproval: Automatic
  name: ${PACKAGE_NAME}
  source: infinispan
  sourceNamespace: ${OPERATOR_NAMESPACE}
EOF
