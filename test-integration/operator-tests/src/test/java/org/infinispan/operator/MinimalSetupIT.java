package org.infinispan.operator;

import java.io.IOException;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.stream.Collectors;
import java.util.stream.Stream;

import org.apache.http.entity.ContentType;
import org.assertj.core.api.Assertions;
import org.infinispan.Caches;
import org.infinispan.Infinispan;
import org.infinispan.Infinispans;
import org.infinispan.TestServer;
import org.infinispan.identities.Credentials;
import org.infinispan.util.CleanUpValidator;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

import cz.xtf.client.Http;
import cz.xtf.core.http.Https;
import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import cz.xtf.junit5.annotations.CleanBeforeAll;
import io.fabric8.kubernetes.api.model.Pod;
import lombok.extern.slf4j.Slf4j;

/**
 * MinimalSetupIT
 *
 * Tests all the defaults as minimal infinispan cluster deployment was created.
 * It is expected that endpoints will be unencrypted with default authentication.
 *
 * spec:
 *   expose:
 *     type: Route
 *   replicas: 2
 *   security:
 *     endpointEncryption:
 *       type: None
 */
@Slf4j
@CleanBeforeAll
class MinimalSetupIT {
   private static final OpenShift openShift = OpenShifts.master();
   private static final Infinispan infinispan = Infinispans.minimalSetup();
   private static final TestServer testServer = TestServer.get();
   private static final String testServerHost = testServer.host();

   private static String appName;
   private static String hostName;

   private static String user;
   private static String pass;

   @BeforeAll
   static void deployInfinispanCluster() throws IOException {
      appName =  infinispan.getClusterName();
      hostName = openShift.generateHostname(appName + "-external");

      infinispan.deploy();
      testServer.deploy();

      infinispan.waitFor();
      testServer.waitFor();

      Credentials developer = infinispan.getDefaultCredentials();
      user = developer.getUsername();
      pass = developer.getPassword();

      Https.doesUrlReturnOK("http://" + testServerHost + "/ping").waitFor();
      Https.doesUrlReturnCode("http://" + hostName, 200).waitFor();
   }

   /**
    * Ensure that all resource created by Operator in this tests are deleted.
    */
   @AfterAll
   static void undeploy() throws IOException {
      infinispan.delete();

      new CleanUpValidator(openShift, appName).withExposedRoute().withDefaultCredentials().withOpenShiftCerts().validate();
   }

   /**
    * Clustering should work out of the box in the OpenShift environment.
    */
   @Test
   void clusteringTest() throws Exception{
      // Create entry through REST
      String cacheUrl = "http://" + hostName + "/rest/v2/caches/cluster-test/";
      String keyUrl = cacheUrl + "cluster-test-key";

      Http.post(cacheUrl).basicAuth(user, pass).preemptiveAuth().data(Caches.fragile("cluster-test"), ContentType.APPLICATION_XML).trustAll().execute();
      Http.put(keyUrl).basicAuth(user, pass).preemptiveAuth().data("cluster-test-value", ContentType.TEXT_PLAIN).trustAll().execute();

      // Validate that entry is available through HotRod directly accessing each node
      String encodedPass = URLEncoder.encode(pass, StandardCharsets.UTF_8.toString());

      List<Pod> pods = openShift.pods().withLabel("clusterName", appName).list().getItems();
      List<String> ips = pods.stream().map(p -> p.getStatus().getPodIP()).collect(Collectors.toList());

      String getNode0 = String.format("http://" + testServerHost + "/hotrod/cluster?username=%s&password=%s&servicename=%s", user, encodedPass, ips.get(0));
      String getNode1 = String.format("http://" + testServerHost + "/hotrod/cluster?username=%s&password=%s&servicename=%s", user, encodedPass, ips.get(1));

      Assertions.assertThat(Http.get(getNode0).execute().code()).isEqualTo(200);
      Assertions.assertThat(Http.get(getNode1).execute().code()).isEqualTo(200);
   }

   /**
    * Verifies valid default authentication configuration for rest protocol.
    */
   @Test
   void restAuthTest() throws Exception {
      String cacheUrl = "http://" + hostName + "/rest/v2/caches/rest-auth-test/";
      String keyUrl = cacheUrl + "authorized-rest-key";

      Http authorizedCachePut = Http.post(cacheUrl).basicAuth(user, pass).preemptiveAuth().data(Caches.fragile("rest-auth-test"), ContentType.APPLICATION_XML).trustAll();
      Http authorizedKeyPut = Http.put(keyUrl).basicAuth(user, pass).preemptiveAuth().data("credentials", ContentType.TEXT_PLAIN).trustAll();
      Http unauthorizedPut = Http.post(cacheUrl).basicAuth(user, "DenitelyNotAPass").preemptiveAuth().data(Caches.fragile("rest-auth-test"), ContentType.APPLICATION_XML).trustAll();
      Http noAuthPut = Http.post(cacheUrl).data(Caches.fragile("rest-auth-test"), ContentType.APPLICATION_XML).trustAll();

      Assertions.assertThat(authorizedCachePut.execute().code()).isEqualTo(200);
      Assertions.assertThat(authorizedKeyPut.execute().code()).isEqualTo(204);
      Assertions.assertThat(unauthorizedPut.execute().code()).isEqualTo(401);
      Assertions.assertThat(noAuthPut.execute().code()).isEqualTo(401);
   }

   /**
    * Verifies valid default authentication configuration for hotrod protocol.
    */
   @Test
   void hotrodAuthTest() throws Exception {
      String encodedPass = URLEncoder.encode(pass, StandardCharsets.UTF_8.toString());

      String authorizedGet = String.format("http://" + testServerHost + "/hotrod/auth?username=%s&password=%s&servicename=%s", user, encodedPass, appName);
      String unauthorizedGet = String.format("http://" + testServerHost + "/hotrod/auth?username=%s&password=%s&servicename=%s", user, "invalid", appName);
      String noAuthGet = String.format("http://" + testServerHost + "/hotrod/auth?servicename=%s", appName);

      Assertions.assertThat(Http.get(authorizedGet).execute().code()).isEqualTo(200);
      Assertions.assertThat(Http.get(unauthorizedGet).execute().code()).isEqualTo(401);
      Assertions.assertThat(Http.get(noAuthGet).execute().code()).isEqualTo(401);
   }

   /**
    * Verify that default cache was created and is accessible.
    */
   @Test
   void defaultCacheAvailabilityTest() throws Exception {
      String keyUrl = "http://" + hostName + "/rest/v2/caches/default/availability-test";

      Http put = Http.put(keyUrl).basicAuth(user, pass).preemptiveAuth().trustAll().data("default-cache-value", ContentType.TEXT_PLAIN);
      Http get = Http.get(keyUrl).basicAuth(user, pass).preemptiveAuth().trustAll();

      Assertions.assertThat(put.execute().code()).isEqualTo(204);
      Assertions.assertThat(get.execute().response()).isEqualTo("default-cache-value");
   }

   /**
    * Default replication factor should be 2
    */
   @Test
   void defaultReplicationFactorTest() throws Exception {
      String request = "http://" + hostName + "/rest/v2/caches/default?action=config";
      String config = Http.get(request).basicAuth(user, pass).trustAll().execute().response();
      String numOwners = Stream.of(config.split(",")).filter(s -> s.contains("owners")).map(s -> s.trim().split(":")[1].trim()).findFirst().orElse("-1");

      Assertions.assertThat(numOwners).isEqualTo("2");
   }
}
