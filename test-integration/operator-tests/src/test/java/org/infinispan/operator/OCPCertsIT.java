package org.infinispan.operator;

import java.io.IOException;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import java.util.stream.Collectors;

import io.fabric8.kubernetes.api.model.Pod;
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
import lombok.extern.slf4j.Slf4j;

/**
 * CacheServiceIT tests default security configurations and Cache Service specific features.
 *
 * Check cache_service.yaml Infinispan CR in test resources for input configuration.
 */
@Slf4j
@CleanBeforeAll
class OCPCertsIT {
   private static final OpenShift openShift = OpenShifts.master();
   private static final Infinispan infinispan = Infinispans.ocpCerts();
   private static final TestServer testServer = TestServer.get();

   private static String appName;
   private static String hostName;

   private static String user;
   private static String pass;

   @BeforeAll
   static void deploy() throws Exception {
      appName = infinispan.getClusterName();
      hostName = openShift.generateHostname(appName + "-external");

      infinispan.deploy();
      testServer.withSecret(appName + "-cert-secret").deploy();

      testServer.waitFor();
      infinispan.waitFor();

      Credentials developer = infinispan.getDefaultCredentials();
      user = developer.getUsername();
      pass = developer.getPassword();

      log.info("Username: {}", user);
      log.info("Password: {}", pass);

      Https.doesUrlReturnOK("http://" + testServer.host() + "/ping").waitFor();
      Https.doesUrlReturnCode("https://" + hostName + "/console/welcome", 200).interval(TimeUnit.SECONDS, 10).waitFor();
   }

   /**
    * Ensure that all resource created by Operator in this tests are deleted.
    */
   @AfterAll
   static void undeploy() throws IOException {
      infinispan.delete();
      testServer.delete();

      new CleanUpValidator(openShift, appName).withExposedRoute().withDefaultCredentials().withOpenShiftCerts().withServiceMonitor().validate();

      openShift.events().delete();
   }

   /**
    * Verifies valid default authentication configuration for rest protocol.
    */
   @Test
   void restAuthTest() throws Exception {
      String cacheUrl = "https://" + hostName + "/rest/v2/caches/rest-auth-test/";
      String keyUrl = cacheUrl + "authorized-rest-key";

      Http authorizedCachePut = Http.post(cacheUrl).basicAuth(user, pass).data(Caches.fragile("rest-auth-test"), ContentType.APPLICATION_XML).trustAll();
      Http authorizedKeyPut = Http.put(keyUrl).basicAuth(user, pass).data("credentials", ContentType.TEXT_PLAIN).trustAll();
      Http unauthorizedPut = Http.post(cacheUrl).basicAuth(user, "DenitelyNotAPass").data(Caches.fragile("rest-auth-test"), ContentType.APPLICATION_XML).trustAll();
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

      String authorizedGet = String.format("http://" + testServer.host() + "/hotrod/auth?username=%s&password=%s&servicename=%s&namespace=%s&encrypted=%s", user, encodedPass, appName, openShift.getNamespace(), "true");
      String unauthorizedGet = String.format("http://" + testServer.host() + "/hotrod/auth?username=%s&password=%s&servicename=%s&namespace=%s&encrypted=%s", user, "invalid", appName, openShift.getNamespace(), "true");
      String noAuthGet = String.format("http://" + testServer.host() + "/hotrod/auth?servicename=%s&namespace=%s&encrypted=%s", appName, openShift.getNamespace(), "true");

      Assertions.assertThat(Http.get(authorizedGet).execute().code()).isEqualTo(200);
      Assertions.assertThat(Http.get(unauthorizedGet).execute().code()).isEqualTo(401);
      Assertions.assertThat(Http.get(noAuthGet).execute().code()).isEqualTo(401);
   }

   /**
    * Default AntiAffinity settings should prevent scheduling all three nodes on the same OCP host.
    * It's required to have OCP cluster with at least 3 worker nodes for the test to pass
    */
   @Test
   void antiAffinityTest() {
      Map<String, String> labels = new HashMap<>();
      labels.put("app", "infinispan-pod");
      labels.put("clusterName", appName);

      List<Pod> clusterPods = openShift.pods().withLabels(labels).list().getItems();
      Set<String> nodeNames = clusterPods.stream().map(p -> p.getSpec().getNodeName()).collect(Collectors.toSet());

      Assertions.assertThat(nodeNames).hasSize(3);
   }
}