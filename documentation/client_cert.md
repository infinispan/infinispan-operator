# Client Certification

## Server
The Infinispan server supports two types of client certification:

1. VALIDATE - Only requires the certificates used to sign the client certificates to be in the Truststore. Typically this is the Certificate Authority (CA).
2. AUTHENTICATE - Requires all of the client certificates to be in the Truststore.

### Configuration
To use either 1 or 2, you need to configure `<endpoints socket-binding="..." security-realm="..." require-ssl-client-auth="true">`

and you need to add `<truststore path="{truststore.path}" password="{truststore.password}"/>` to `<server-identities><ssl>..`.

To enable 2, you also need to add a `<truststore-realm/>` element to the `security-realm`.

For example:

```xml
<server>
    <security xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
            xsi:schemaLocation="urn:infinispan:server:12.1 https://infinispan.org/schemas/infinispan-server-12.1.xsd"
            xmlns="urn:infinispan:server:12.1">
    <security-realms>
        <security-realm name="default">
            <server-identities>
                <ssl>
                <keystore path="server.pfx" relative-to="infinispan.server.config.path" keystore-password="secret"
                            alias="server"/>
                <truststore path="ca.pfx"  relative-to="infinispan.server.config.path" password="secret"/>
                </ssl>
            </server-identities>
            <!-- Optional element that should be added if client cert AUTHENTICATION is required -->
            <truststore-realm/>
        </security-realm>
    </security-realms>
    </security>
    <endpoints xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
            xsi:schemaLocation="urn:infinispan:server:12.1 https://infinispan.org/schemas/infinispan-server-12.1.xsd"
            xmlns="urn:infinispan:server:12.1"
            socket-binding="default" security-realm="default" require-ssl-client-auth="true"/>

<server>
```

## Clients

### Hot Rod
A keystore containing the client certificate must be configured on HotRod clients using the [EXTERNAL](https://infinispan.org/docs/stable/titles/hotrod_java/hotrod_java.html#hotrod_endpoint_auth-client) mechanism.

### Rest
The client cert must be associated with any REST call made by the client.

## Operator
The default is for no client certification to be enabled, which is equivalent to `require-ssl-client-auth="false"` on the server.

The operator supports both client cert authentication and validation, which can be configured as below:

```yaml
spec:
  security:
    endpointEncryption:
        type: Secret | Service
        certSecretName: tls-secret 
        clientCert: None | Validate | Authenticate
        # Optional name. If not specified then secret created as "<cr-name>-client-cert-secret"
        clientCertSecretName: truststore-secret
```

The user can either provide an existing Truststore, or relying on the operator to create one by providing PEM encoded certificates.

> Only pkcs12 format Truststores are supported.

### User Provided Truststore
The user can provide and existing truststore via the client cert secret in a similar manner to how `keystore.p12` can be configured in the tls-secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: truststore-secret
type: Opaque
stringData:
    truststore-password: password 
data:
    truststore.p12:  "ALsadasdaFASfASFaSfasfASfasfa..." 
```

### Generated Truststore
Alternatively the user can provide PEM encoded certificates via the client cert secret and the truststore is generated on their behalf.

The `trust.ca` data should be a PEM CA bundle of certificates used to sign the client certificates. This is mandatory for both cert Authentication and Validation.

The `trust.cert.*` files corresponds to individual client certificates. This is only necessary for cert Authentication.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: truststore-secret
type: Opaque
stringData:
    truststore-password: password 
data:
    trust.ca: "Some ca byte string"
    trust.cert.client1: "Some cert byte string"
    trust.cert.client2: "Some cert byte string"
```

> The `trust.cert.<name>` value is used as the alias associated with the certificate in the generated truststore.

> The `truststore-password` field is optional. If a truststore value is provided by the user, then it is applied to the generated Truststore,
otherwise a default value of "password" is used and added to the secret.
