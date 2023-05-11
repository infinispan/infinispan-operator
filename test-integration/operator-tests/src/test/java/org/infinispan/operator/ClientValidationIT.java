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

   private static String appName;
   private static final String testServerName = "test-server";
   private static String user;
   private static String pass;

   @BeforeAll
   static void deploy() throws IOException {
      appName = infinispan.getClusterName();
      String hostName = openShift.generateHostname(appName + "-external");
      KeystoreGenerator.CertPaths ipsnCerts = KeystoreGenerator.generateCerts(hostName, new String[]{appName});
      KeystoreGenerator.CertPaths testServerCerts = KeystoreGenerator.generateCerts(testServerHost, new String[]{testServerName});

      Secret ispnEncryptionSecret = new SecretBuilder("encryption-secret").addLabel("test", "ClientValidationIT")
              .addData("keystore.p12", ipsnCerts.keystore)
              .addData("alias", hostName.getBytes())
              .addData("password", "password".getBytes()).build();
      Secret ispnAuthSecret = new SecretBuilder("connect-secret").addLabel("test", "ClientValidationIT")
              .addData("identities.yaml", Paths.get("src/test/resources/secrets/identities.yaml")).build();
      Secret ispnClientValidationSecret = new SecretBuilder(appName + "-client-cert-secret").addLabel("test", "ClientValidationIT")
              .addData("truststore.p12", KeystoreGenerator.getTruststore())
              .addData("truststore-password", "password".getBytes()).build();

      Secret testServerEncryptionSecret = new SecretBuilder("test-server-cert-secret").addLabel("test", "ClientValidationIT")
              .addData("keystore.p12", testServerCerts.keystore)
              .addData("truststore.p12", KeystoreGenerator.getTruststore()).build();

      openShift.secrets().create(ispnEncryptionSecret);
      openShift.secrets().create(ispnAuthSecret);
      openShift.secrets().create(ispnClientValidationSecret);
      openShift.secrets().create(testServerEncryptionSecret);

      infinispan.deploy();
      testServer.withSecret("test-server-cert-secret").deploy();

      infinispan.waitFor();
      testServer.waitFor();

      Credentials developer = infinispan.getCredentials("testuser");
      user = developer.getUsername();
      pass = developer.getPassword();

      Https.doesUrlReturnOK("http://" + testServerHost + "/ping").waitFor();
   }

   @AfterAll
   static void undeployAndVerifyIfSecretsExistUponDeletion() throws IOException {
      infinispan.delete();
      testServer.delete();

      try{
         Assertions.assertThat(openShift.getSecret("encryption-secret")).isNotNull();
         Assertions.assertThat(openShift.getSecret("connect-secret")).isNotNull();
         Assertions.assertThat(openShift.getSecret(appName + "-client-cert-secret")).isNotNull();
      } finally {
         new CleanUpValidator(openShift, appName).withExposedRoute().validate();
      }

      openShift.secrets().withLabel("test", "ClientAuthenticationIT").delete();
   }

   @Test
   void noCredentialsTest() throws Exception {
      String noCredentialsGet = String.format("http://" + testServerHost + "/hotrod/client/validation?servicename=%s&useKeystores=%s", appName, "true");
      Assertions.assertThat(Http.get(noCredentialsGet).execute().code()).isEqualTo(401);
   }

   @Test
   void noKeystoreTest() throws Exception {
      String encodedPass = URLEncoder.encode(pass, StandardCharsets.UTF_8.toString());
      String credentialsWithoutKeystore = String.format("http://" + testServerHost + "/hotrod/client/validation?username=%s&password=%s&servicename=%s",
              user, encodedPass, appName);
      Assertions.assertThat(Http.get(credentialsWithoutKeystore).execute().code()).isEqualTo(401);
   }

   @Test
   void hotrodValidationTest() throws Exception {
      String encodedPass = URLEncoder.encode(pass, StandardCharsets.UTF_8.toString());
      String authorizedGet = String.format("http://" + testServerHost + "/hotrod/client/validation?username=%s&password=%s&servicename=%s&useKeystores=%s",
              user, encodedPass, appName, "true");
      Assertions.assertThat(Http.get(authorizedGet).execute().code()).isEqualTo(200);
   }
}
