package org.infinispan.operator;

import java.io.IOException;
import java.nio.file.Paths;

import io.fabric8.kubernetes.api.model.Affinity;
import io.fabric8.kubernetes.api.model.apps.StatefulSet;
import org.apache.http.entity.ContentType;
import org.assertj.core.api.Assertions;
import org.infinispan.Caches;
import org.infinispan.Infinispan;
import org.infinispan.Infinispans;
import org.infinispan.client.hotrod.RemoteCache;
import org.infinispan.client.hotrod.RemoteCacheManager;
import org.infinispan.client.hotrod.configuration.ClientIntelligence;
import org.infinispan.client.hotrod.configuration.ConfigurationBuilder;
import org.infinispan.client.hotrod.exceptions.HotRodClientException;
import org.infinispan.commons.configuration.XMLStringConfiguration;
import org.infinispan.identities.Credentials;
import org.infinispan.util.CleanUpValidator;
import org.infinispan.util.KeystoreGenerator;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

import cz.xtf.builder.builders.SecretBuilder;
import cz.xtf.client.Http;
import cz.xtf.core.http.Https;
import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import cz.xtf.junit5.annotations.CleanBeforeAll;
import io.fabric8.kubernetes.api.model.Pod;
import io.fabric8.kubernetes.api.model.Secret;
import lombok.extern.slf4j.Slf4j;

/**
 * Compared to MinimalSetupIT this set of tests are running
 * against features that need to be configured.
 *
 * Check datagrid_service.yaml Infinispan CR in test resources for input configuration.
 */
@Slf4j
@CleanBeforeAll
class DataGridServiceIT {
   private static final OpenShift openShift = OpenShifts.master();
   private static final Infinispan infinispan = Infinispans.dataGridService();

   private static KeystoreGenerator.CertPaths certs;
   private static String appName;
   private static String hostName;
   private static String user;
   private static String pass;

   @BeforeAll
   static void deploy() throws Exception {
      appName = infinispan.getClusterName();
      hostName = openShift.generateHostname(appName + "-external");

      certs = KeystoreGenerator.generateCerts(hostName, new String[]{appName});

      Secret encryptionSecret = new SecretBuilder("encryption-secret")
            .addData("keystore.p12", certs.keystore)
            .addData("alias", hostName.getBytes())
            .addData("password", "password".getBytes()).build();
      Secret authSecret = new SecretBuilder("connect-secret")
            .addData("identities.yaml", Paths.get("src/test/resources/secrets/identities.yaml")).build();

      openShift.secrets().create(encryptionSecret);
      openShift.secrets().create(authSecret);

      infinispan.deploy();
      infinispan.waitFor();

      Credentials developer = infinispan.getCredentials("testuser");
      user = developer.getUsername();
      pass = developer.getPassword();

      Https.doesUrlReturnCode("https://" + hostName, 200).waitFor();
   }


   /**
    * Ensure that all resource created by Operator in this tests are deleted.
    */
   @AfterAll
   static void undeploy() throws IOException {
      infinispan.delete();

      new CleanUpValidator(openShift, appName).withExposedRoute().withServiceMonitor().validate();
   }

   /**
    * Default cache should be available only for Cache type of service
    */
   @Test
   void defaultCacheNotPresentTest() throws IOException {
      String cacheUrl = "https://" + hostName + "/rest/v2/caches/default/";
      Http get = Http.get(cacheUrl).basicAuth(user, pass).trustAll();
      Assertions.assertThat(get.execute().code()).isEqualTo(404);
   }

   /**
    * Verify that logging configuration is properly reflected in logs.
    */
   @Test
   void loggingTest() {
      Pod node = openShift.pods().withLabel("clusterName", appName).list().getItems().stream().findFirst().orElseThrow(() -> new IllegalStateException("Data Grid nodes are missing!"));

      String log = openShift.getPodLog(node);

      Assertions.assertThat(log).contains("DEBUG (main) [org.jgroups");
      Assertions.assertThat(log).doesNotContain("INFO (main) [org.infinispan");
   }

   /**
    * Verifies valid default authentication configuration for rest protocol.
    */
   @Test
   void restTest() throws IOException {
      String cacheUrl = "https://" + hostName + "/rest/v2/caches/rest-auth-test/";
      String keyUrl = cacheUrl + "authorized-rest-key";

      Http authorizedCachePut = Http.post(cacheUrl).basicAuth(user, pass).data(Caches.fragile("rest-auth-test"), ContentType.APPLICATION_XML).trustStore(KeystoreGenerator.getTruststore(), "password");
      Http authorizedKeyPut = Http.put(keyUrl).basicAuth(user, pass).data("credentials", ContentType.TEXT_PLAIN).trustStore(KeystoreGenerator.getTruststore(), "password");
      Http unauthorizedPut = Http.post(cacheUrl).basicAuth(user, "DenitelyNotAPass").data(Caches.fragile("rest-auth-test"), ContentType.APPLICATION_XML).trustStore(KeystoreGenerator.getTruststore(), "password");
      Http noAuthPut = Http.post(cacheUrl).data(Caches.fragile("rest-auth-test"), ContentType.APPLICATION_XML).trustStore(KeystoreGenerator.getTruststore(), "password");

      Assertions.assertThat(authorizedCachePut.execute().code()).isEqualTo(200);
      Assertions.assertThat(authorizedKeyPut.execute().code()).isEqualTo(204);
      Assertions.assertThat(unauthorizedPut.execute().code()).isEqualTo(401);
      Assertions.assertThat(noAuthPut.execute().code()).isEqualTo(401);
   }

   /**
    * Verifies valid default authentication configuration for hotrod protocol.
    */
   @Test
   void hotrodTest() {
      RemoteCache<String, String> rc = getConfiguration(user, pass).administration().getOrCreateCache("testcache", new XMLStringConfiguration(Caches.testCache()));
      rc.put("hotrod-encryption-external", "hotrod-encryption-value");
      Assertions.assertThat(rc.get("hotrod-encryption-external")).isEqualTo("hotrod-encryption-value");

      Assertions.assertThatThrownBy(
            () -> getConfiguration(user, "NotAPass").administration().getOrCreateCache("testcache", new XMLStringConfiguration(Caches.testCache()))
      ).isInstanceOf(HotRodClientException.class);

      Assertions.assertThatThrownBy(
            () -> getConfiguration(null, null).administration().getOrCreateCache("testcache", new XMLStringConfiguration(Caches.testCache()))
      ).isInstanceOf(HotRodClientException.class);
   }

   /**
    * Verifies that AntiAffinity settings get propagated to StatefulSet.
    */
   @Test
   void antiAffinityTest() {
      StatefulSet ss = openShift.getStatefulSet(appName);
      Affinity affinity = ss.getSpec().getTemplate().getSpec().getAffinity();

      Assertions.assertThat(affinity).isNotNull();
      Assertions.assertThat(affinity.getPodAntiAffinity().getRequiredDuringSchedulingIgnoredDuringExecution()).isNotNull();
   }

   private RemoteCacheManager getConfiguration(String username, String password) {
      String truststore = certs.truststore.toAbsolutePath().toString();

      ConfigurationBuilder builder = new ConfigurationBuilder();
      builder.maxRetries(1);
      builder.addServer().host(hostName).port(443);
      builder.security().ssl().sniHostName(hostName).trustStoreFileName(truststore).trustStorePassword("password".toCharArray());
      builder.clientIntelligence(ClientIntelligence.BASIC);

      if (username != null && password != null) {
         builder.security().authentication().realm("default").serverName("infinispan").username(username).password(password).enable();
      }

      return new RemoteCacheManager(builder.build());
   }
}
