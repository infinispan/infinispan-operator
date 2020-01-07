echo "Paste your quay.io password:"
export QUAY_TOKEN=$(curl --silent -H "Content-Type: application/json" -XPOST https://quay.io/cnr/api/v1/users/login -d '{"user":{"username":"'"${QUAY_USERNAME}"'","password":"'"$(read -s PW && echo -n $PW)"'"}}' | sed -E 's/.*\"(basic .*)\".*/\1/')
