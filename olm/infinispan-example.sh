OPERATOR_NAMESPACE=$1
cat <<EOF | oc apply -f -
apiVersion: infinispan.org/v1
kind: Infinispan
metadata:
  name: example-infinispan
  namespace: ${OPERATOR_NAMESPACE}
spec:
  replicas: 1
EOF
