package org.infinispan.operator;

import java.io.IOException;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.Base64;
import java.util.concurrent.TimeUnit;
import java.util.function.BooleanSupplier;

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
import org.infinispan.commons.configuration.XMLStringConfiguration;
import org.infinispan.identities.Credentials;
import org.infinispan.util.KeystoreGenerator;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

import cz.xtf.builder.builders.RouteBuilder;
import cz.xtf.builder.builders.SecretBuilder;
import cz.xtf.client.Http;
import cz.xtf.client.HttpResponseParser;
import cz.xtf.core.http.Https;
import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import cz.xtf.core.waiting.SimpleWaiter;
import cz.xtf.junit5.annotations.CleanBeforeAll;
import io.fabric8.kubernetes.api.model.Secret;
import io.fabric8.kubernetes.api.model.Service;
import io.fabric8.openshift.api.model.Route;
import lombok.extern.slf4j.Slf4j;

/**
 * AdvancedSetupB compared to AdvancedSetupA tests endpoint encrypted by OpenShift.
 *
 * spec:
 *   expose:
 *     type: LoadBalancer
 *   security:
 *     endpointEncryption:
 *       type: service
 *       certServiceName: service.beta.openshift.io
 *       certSecretName: tls-secret
 *   replicas: 1
 *
 */
@Slf4j
@CleanBeforeAll
public class AdvancedSetupBIT {
   private static final OpenShift openShift = OpenShifts.master();
   private static final Infinispan infinispan = Infinispans.advancedSetupB();
   private static final TestServer testServer = TestServer.get();

   private static String appName;
   private static String hostName;

   private static String user;
   private static String pass;

   @BeforeAll
   public static void deploy() throws Exception {
      testServer.deploy();
      infinispan.deploy();

      testServer.waitFor();
      infinispan.waitFor();

      Credentials developer = infinispan.getDefaultCredentials();
      user = developer.getUsername();
      pass = developer.getPassword();

      appName = infinispan.getClusterName();

      Https.doesUrlReturnOK("http://" + testServer.host() + "/ping").waitFor();

      Service external = openShift.services().withName(appName + "-external").get();
      hostName = external.getStatus().getLoadBalancer().getIngress().get(0).getHostname();

      Https.doesUrlReturnCode("https://" + hostName + ":11222/console/welcome", 200).interval(TimeUnit.SECONDS, 10).waitFor();
   }

   /**
    * Ensure that all resource created by Operator in this tests are deleted.
    */
   @AfterAll
   static void undeploy() throws IOException {
      infinispan.delete();

      BooleanSupplier bs = () -> openShift.apps().statefulSets().withName(appName).get() == null &&
            openShift.services().withName(appName).get() == null &&
            openShift.services().withName(appName + "-ping").get() == null &&
            openShift.services().withName(appName + "-external").get() == null &&
            openShift.secrets().withName(appName + "-generated-secret").get() == null &&
            openShift.configMaps().withName(appName + "-configuration").get() == null &&
            openShift.pods().withLabel("clusterName", appName).list().getItems().isEmpty() &&
            openShift.persistentVolumeClaims().withLabel("clusterName", appName).list().getItems().isEmpty();

      new SimpleWaiter(bs, "Waiting for resource deletion").waitFor();
   }

   /**
    * Verify that default cache was created and is accessible through exposed LoadBalancer.
    * We need to trust all the certificates as used are valid only for OpenShifts internal communication.
    */
   @Test
   void defaultCacheAvailabilityTest() throws Exception {
      String keyUrl = "https://" + hostName + ":11222/rest/v2/caches/default/availability-test";

      Http put = Http.put(keyUrl).basicAuth(user, pass).data("default-cache-value", ContentType.TEXT_PLAIN).trustAll();
      Http get = Http.get(keyUrl).basicAuth(user, pass).trustAll();

      Assertions.assertThat(put.execute().code()).isEqualTo(204);
      Assertions.assertThat(get.execute().response()).isEqualTo("default-cache-value");
   }

   /**
    * Executes HotRod client from within Tomcat container running on OpenShift with usage of service-ca.crt file.
    */
   @Test
   void hotrodInternalAccessTest() throws Exception {
      String encodedPass = URLEncoder.encode(pass, StandardCharsets.UTF_8.toString());
      String url = "http://" + testServer.host() + "/hotrod/encryption-provided?username=%s&password=%s&servicename=%s";
      String get = String.format(url, user, encodedPass, appName);

      HttpResponseParser response = Http.get(get).execute();

      Assertions.assertThat(response.code()).as(response.response()).isEqualTo(200);
   }
}