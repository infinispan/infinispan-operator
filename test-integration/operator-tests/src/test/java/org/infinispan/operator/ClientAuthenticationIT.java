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
import org.infinispan.util.CleanUpValidator;
import org.infinispan.util.KeystoreGenerator;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

import java.io.IOException;

@CleanBeforeAll
class ClientAuthenticationIT {
   private static final OpenShift openShift = OpenShifts.master();
   private static final Infinispan infinispan = Infinispans.clientTlsAuthentication();
   private static final TestServer testServer = TestServer.get();
   private static final String testServerHost = testServer.host();

   private static String appName;
   private static final String testServerName = "test-server";

   @BeforeAll
   static void deploy() throws IOException {
      appName = infinispan.getClusterName();
      String hostName = openShift.generateHostname(appName + "-external");
      KeystoreGenerator.CertPaths ipsnCerts = KeystoreGenerator.generateCerts(hostName, new String[]{appName});
      KeystoreGenerator.CertPaths testServerCerts = KeystoreGenerator.generateCerts(testServerHost, new String[]{testServerName});
      KeystoreGenerator.CertPaths unknownCerts = KeystoreGenerator.generateCerts("unknownHostname");

      Secret ispnEncryptionSecret = new SecretBuilder("encryption-secret").addLabel("test", "ClientAuthenticationIT")
              .addData("keystore.p12", ipsnCerts.keystore)
              .addData("alias", hostName.getBytes())
              .addData("password", "password".getBytes())
              .build();
      Secret ispnClientValidationSecret = new SecretBuilder(appName + "-client-cert-secret").addLabel("test", "ClientAuthenticationIT")
              .addData("trust.ca", testServerCerts.caPem)
              .addData("trust.cert.client", testServerCerts.certPem)
              .build();

      Secret testServerEncryptionSecret = new SecretBuilder("test-server-cert-secret").addLabel("test", "ClientAuthenticationIT")
              .addData("keystore.p12", testServerCerts.keystore)
              .addData("truststore.p12", KeystoreGenerator.getTruststore())
              .build();
      Secret testServerUnknownByServerSecret = new SecretBuilder("unknown-cert-secret").addLabel("test", "ClientAuthenticationIT")
              .addData("keystore.p12", unknownCerts.keystore)
              .addData("truststore.p12", unknownCerts.truststore)
              .build();

      openShift.secrets().create(ispnEncryptionSecret);
      openShift.secrets().create(ispnClientValidationSecret);
      openShift.secrets().create(testServerEncryptionSecret);
      openShift.secrets().create(testServerUnknownByServerSecret);

      infinispan.deploy();
      testServer.withSecret("test-server-cert-secret")
              .withSecret("unknown-cert-secret").deploy();

      infinispan.waitFor();
      testServer.waitFor();
      Https.doesUrlReturnOK("http://" + testServerHost + "/ping").waitFor();
   }

   @AfterAll
   static void undeployAndVerifyIfSecretsExistUponDeletion() throws IOException {
      infinispan.delete();
      testServer.delete();

      try{
         Assertions.assertThat(openShift.getSecret("encryption-secret")).isNotNull();
         Assertions.assertThat(openShift.getSecret(appName + "-client-cert-secret")).isNotNull();
      } finally {
         new CleanUpValidator(openShift, appName).withExposedRoute().validate();
      }

      openShift.secrets().withLabel("test", "ClientAuthenticationIT").delete();
      openShift.events().delete();
   }

   @Test
   void noKeystoreTest() throws Exception {
      String credentialsWithoutKeystore = String.format("http://" + testServerHost + "/hotrod/client/authentication?servicename=%s", appName);
      Assertions.assertThat(Http.get(credentialsWithoutKeystore).execute().code()).isEqualTo(401);
   }

   @Test
   void hotrodAuthenticationTest() throws Exception {
      String authorizedGet = String.format("http://" + testServerHost + "/hotrod/client/authentication?servicename=%s&secretName=%s", appName, "test-server-cert-secret");
      Assertions.assertThat(Http.get(authorizedGet).execute().code()).isEqualTo(200);
   }

   @Test
   void hotrodUnknownClientCertTest() throws Exception {
      String authorizedGet = String.format("http://" + testServerHost + "/hotrod/client/authentication?servicename=%s&secretName=%s", appName, "unknown-cert-secret");
      Assertions.assertThat(Http.get(authorizedGet).execute().code()).isEqualTo(401);
   }
}
