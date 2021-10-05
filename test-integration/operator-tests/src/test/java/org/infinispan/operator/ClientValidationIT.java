package org.infinispan.operator;

import cz.xtf.builder.builders.SecretBuilder;
import cz.xtf.client.Http;
import cz.xtf.core.http.Https;
import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import cz.xtf.junit5.annotations.CleanBeforeAll;
import io.fabric8.kubernetes.api.model.Secret;
import org.assertj.core.api.Assertions;
import org.infinispan.Infinispan;
import org.infinispan.Infinispans;
import org.infinispan.TestServer;
import org.infinispan.identities.Credentials;
import org.infinispan.util.CleanUpValidator;
import org.infinispan.util.KeystoreGenerator;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.nio.file.Paths;
import java.util.Map;

/**
 * ClientValidationIT tests if there is client validation strategy enabled. To validate client certificates,
 * Data Grid requires a trust store that contains any part of the certificate chain for the signing authority,
 * typically the root CA certificate. Any client that presents a certificate signed by the CA can connect to Data Grid.
 */
@CleanBeforeAll
class ClientValidationIT {
   private static final OpenShift openShift = OpenShifts.master();
   private static final Infinispan infinispan = Infinispans.clientTlsValidation();
   private static final TestServer testServer = TestServer.get();
   private static final String testServerHost = testServer.host();

   private static KeystoreGenerator.CertPaths certs;
   private static String appName;
   private static String hostName;
   private static String user;
   private static String pass;

   @BeforeAll
   static void deploy() throws IOException {
      appName = infinispan.getClusterName();
      hostName = openShift.generateHostname(appName + "-external");

      certs = KeystoreGenerator.generateCerts(hostName, new String[]{appName});

      Secret encryptionSecret = new SecretBuilder("encryption-secret")
              .addData("keystore.p12", certs.keystore)
              .addData("alias", hostName.getBytes())
              .addData("password", "password".getBytes())
              .build();

      Secret authSecret = new SecretBuilder("connect-secret")
              .addData("identities.yaml", Paths.get("src/test/resources/secrets/identities.yaml"))
              .build();

      Secret clientValidationSecret = new SecretBuilder(appName + "-client-cert-secret")
              .addData("truststore.p12", certs.truststore)
              .addData("truststore-password", "password".getBytes())
              .build();

      openShift.secrets().create(encryptionSecret);
      openShift.secrets().create(authSecret);
      openShift.secrets().create(clientValidationSecret);

      infinispan.deploy();
      testServer.withSecret("encryption-secret")
              .withSecret((appName + "-cert-secret"))
              .deploy();

      infinispan.waitFor();
      testServer.waitFor();

      Credentials developer = infinispan.getCredentials("testuser");
      user = developer.getUsername();
      pass = developer.getPassword();

      Https.doesUrlReturnOK("http://" + testServerHost + "/ping").waitFor();
   }

   @AfterAll
   static void undeploy() throws IOException {
      infinispan.delete();
      new CleanUpValidator(openShift, appName).withExposedRoute().validate();
   }

   @Test
   void noCredentialsTest() throws Exception {
      Map<String, Object> spec = (Map<String, Object>) infinispan.getInfinispan().get("spec");
      Map<String, Object> security = (Map<String, Object>) spec.get("security");
      Boolean endpointAuthentication = (Boolean) security.get("endpointAuthentication");
      Assertions.assertThat(endpointAuthentication).isEqualTo(true);

      String noCredentialsGet = String.format("http://" + testServerHost + "/hotrod/auth?servicename=%s&encrypted=%s&clientValidation=%s", appName, "true", "true");
      Assertions.assertThat(Http.get(noCredentialsGet).execute().code()).isEqualTo(401);
   }

   @Test
   void noKeystoreTest() throws Exception {
      Map<String, Object> spec = (Map<String, Object>) infinispan.getInfinispan().get("spec");
      Map<String, Object> security = (Map<String, Object>) spec.get("security");
      Boolean endpointAuthentication = (Boolean) security.get("endpointAuthentication");
      Assertions.assertThat(endpointAuthentication).isEqualTo(true);

      String encodedPass = URLEncoder.encode(pass, StandardCharsets.UTF_8.toString());
      String credentialsWithoutKeystore = String.format("http://" + testServerHost + "/hotrod/auth?username=%s&password=%s&servicename=%s&encrypted=%s",
              user, encodedPass, appName, "true");
      Assertions.assertThat(Http.get(credentialsWithoutKeystore).execute().code()).isEqualTo(401);
   }

   @Test
   void hotrodValidationTest() throws Exception {
      String encodedPass = URLEncoder.encode(pass, StandardCharsets.UTF_8.toString());
      String authorizedGet = String.format("http://" + testServerHost + "/hotrod/auth?username=%s&password=%s&servicename=%s&encrypted=%s&clientValidation=%s",
              user, encodedPass, appName, "true", "true");
      Assertions.assertThat(Http.get(authorizedGet).execute().code()).isEqualTo(200);
   }
}
