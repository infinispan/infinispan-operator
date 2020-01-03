package org.infinispan.operator.security;

import java.io.IOException;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.Base64;

import org.apache.http.entity.ContentType;
import org.assertj.core.api.Assertions;
import org.infinispan.Caches;
import org.infinispan.Infinispan;
import org.infinispan.Infinispans;
import org.infinispan.TestServer;
import org.infinispan.client.hotrod.RemoteCache;
import org.infinispan.client.hotrod.RemoteCacheManager;
import org.infinispan.client.hotrod.configuration.ClientIntelligence;
import org.infinispan.client.hotrod.configuration.ConfigurationBuilder;
import org.infinispan.identities.Credentials;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

import cz.xtf.builder.builders.RouteBuilder;
import cz.xtf.client.Http;
import cz.xtf.client.HttpResponseParser;
import cz.xtf.core.http.Https;
import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import cz.xtf.junit5.annotations.CleanBeforeAll;
import io.fabric8.kubernetes.api.model.Secret;
import io.fabric8.openshift.api.model.Route;
import lombok.extern.slf4j.Slf4j;

@Slf4j
@CleanBeforeAll
class EncryptionProvidedTest {
   private static final OpenShift openShift = OpenShifts.master();
   private static final Infinispan infinispan = Infinispans.encryptionProvided();
   private static final TestServer testServer = TestServer.get();

   private static String appName;
   private static String passthroughHostName;
   private static String user;
   private static String pass;

   @BeforeAll
   static void prepare() throws Exception {
      testServer.deploy();
      infinispan.deploy();

      appName = infinispan.getClusterName();
      passthroughHostName = openShift.generateHostname(appName + "-passthrough");

      Route route = new RouteBuilder(appName).passthrough().forService(appName).targetPort(11222).exposedAsHost(passthroughHostName).build();

      openShift.createRoute(route);

      testServer.waitFor();
      infinispan.waitFor();

      Credentials developer = infinispan.getDefaultCredentials();
      user = developer.getUsername();
      pass = developer.getPassword();

      Https.doesUrlReturnOK("http://" + testServer.host() + "/ping").waitFor();
      Https.doesUrlReturnCode("https://" + passthroughHostName, 200).waitFor();

      Http.post("https://" + passthroughHostName + "/rest/v2/caches/testcache").basicAuth(user, pass).data(Caches.testCache(), ContentType.APPLICATION_XML).trustAll().execute();
   }

   @Test
   void hotrodExternalAccessTest() throws IOException {
      Secret tlsSecret = openShift.getSecret("tls-secret");
      String tlsCrt = tlsSecret.getData().get("tls.crt");

      Path tmp = Paths.get("tmp").toAbsolutePath();
      Path crt = tmp.resolve("tls.crt");

      tmp.toFile().mkdirs();

      Files.write(crt, Base64.getDecoder().decode(tlsCrt));

      ConfigurationBuilder builder = new ConfigurationBuilder();
      builder.addServer().host(passthroughHostName).port(443);
      builder.security().authentication().realm("default").serverName("infinispan").username(user).password(pass).enable();
      builder.security().ssl().sniHostName(passthroughHostName).trustStorePath(crt.toString());
      builder.clientIntelligence(ClientIntelligence.BASIC);

      RemoteCacheManager rcm = new RemoteCacheManager(builder.build());
      RemoteCache<String, String> rc = rcm.getCache("testcache");

      rc.put("hotrod-encryption-external", "hotrod-encryption-value");

      Assertions.assertThat(rc.get("hotrod-encryption-external")).isEqualTo("hotrod-encryption-value");
   }

   @Test
   void hotrodInternalAccessTest() throws Exception {
      String encodedPass = URLEncoder.encode(pass, StandardCharsets.UTF_8.toString());
      String url = "http://" + testServer.host() + "/hotrod/encryption-provided?username=%s&password=%s&servicename=%s";
      String get = String.format(url, user, encodedPass, appName);

      HttpResponseParser response = Http.get(get).execute();

      Assertions.assertThat(response.code()).as(response.response()).isEqualTo(200);
   }
}
