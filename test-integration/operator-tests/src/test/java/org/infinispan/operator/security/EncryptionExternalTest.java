package org.infinispan.operator.security;

import org.apache.http.entity.ContentType;
import org.assertj.core.api.Assertions;
import org.infinispan.Caches;
import org.infinispan.Infinispan;
import org.infinispan.Infinispans;
import org.infinispan.client.hotrod.RemoteCache;
import org.infinispan.client.hotrod.RemoteCacheManager;
import org.infinispan.client.hotrod.configuration.ClientIntelligence;
import org.infinispan.client.hotrod.configuration.ConfigurationBuilder;
import org.infinispan.commons.configuration.XMLStringConfiguration;
import org.infinispan.identities.Credentials;
import org.infinispan.util.KeystoreGenerator;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

import cz.xtf.builder.builders.RouteBuilder;
import cz.xtf.builder.builders.SecretBuilder;
import cz.xtf.client.Http;
import cz.xtf.core.http.Https;
import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import cz.xtf.junit5.annotations.CleanBeforeAll;
import io.fabric8.kubernetes.api.model.Secret;
import io.fabric8.openshift.api.model.Route;

@CleanBeforeAll
class EncryptionExternalTest {
   private static final OpenShift openShift = OpenShifts.master();
   private static final Infinispan infinispan = Infinispans.encryptionExternal();

   private static KeystoreGenerator.CertPaths certs;
   private static String appName;
   private static String hostName;
   private static String user;
   private static String pass;

   @BeforeAll
   static void prepare() throws Exception {
      infinispan.deploy();

      appName = infinispan.getClusterName();
      hostName = openShift.generateHostname(appName);

      certs = KeystoreGenerator.generateCerts(hostName);

      Secret secret = new SecretBuilder("encryption-secret")
            .addData("keystore.p12", certs.keystore)
            .addData("alias", hostName.getBytes())
            .addData("password", "password".getBytes()).build();
      Route route = new RouteBuilder(appName).passthrough().forService(appName).targetPort(11222).exposedAsHost(hostName).build();

      openShift.secrets().create(secret);
      openShift.createRoute(route);

      infinispan.waitFor();

      Credentials developer = infinispan.getDefaultCredentials();
      user = developer.getUsername();
      pass = developer.getPassword();

      Https.doesUrlReturnCode("https://" + hostName, 401).waitFor();
   }

   @Test
   void restExternalAccessTest() throws Exception {
      String cacheUrl = "https://" + hostName + "/rest/v2/caches/testcache";
      String keyUrl = "https://" + hostName + "/rest/testcache/rest-encryption-external";

      Http.post(cacheUrl).basicAuth(user, pass).data(Caches.testCache(), ContentType.APPLICATION_XML).trustStore(certs.truststore, "password").execute();
      Http.put(keyUrl).basicAuth(user, pass).data("rest-encryption-value", ContentType.TEXT_PLAIN).trustStore(certs.truststore, "password").execute();

      String value = Http.get(keyUrl).basicAuth(user, pass).trustStore(certs.truststore, "password").execute().response();

      Assertions.assertThat(value).isEqualTo("rest-encryption-value");
   }

   @Test
   void hotrodExternalAccessTest() {
      String truststore = certs.truststore.toAbsolutePath().toString();

      ConfigurationBuilder builder = new ConfigurationBuilder();
      builder.addServer().host(hostName).port(443);
      builder.security().authentication().realm("default").serverName("infinispan").username(user).password(pass).enable();
      builder.security().ssl().sniHostName(hostName).trustStoreFileName(truststore).trustStorePassword("password".toCharArray());
      builder.clientIntelligence(ClientIntelligence.BASIC);

      RemoteCacheManager rcm = new RemoteCacheManager(builder.build());
      RemoteCache<String, String> rc = rcm.administration().getOrCreateCache("testcache", new XMLStringConfiguration(Caches.testCache()));

      rc.put("hotrod-encryption-external", "hotrod-encryption-value");

      Assertions.assertThat(rc.get("hotrod-encryption-external")).isEqualTo("hotrod-encryption-value");
   }
}
