export TOKEN_A=$(oc login --insecure-skip-tls-verify=true --server=${SERVER_A} --username=${USERNAME_A} --password=${PASSWORD_A} >/dev/null && oc whoami --show-token=true)
export TOKEN_B=$(oc login --insecure-skip-tls-verify=true --server=${SERVER_B} --username=${USERNAME_B} --password=${PASSWORD_B} >/dev/null && oc whoami --show-token=true)
