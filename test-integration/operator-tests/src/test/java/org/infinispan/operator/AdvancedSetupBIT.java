package org.infinispan.operator;

import java.io.IOException;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import java.util.function.BooleanSupplier;
import java.util.stream.Collectors;
import java.util.stream.Stream;

import cz.xtf.core.waiting.SimpleWaiter;
import io.fabric8.kubernetes.api.model.Pod;
import org.apache.commons.lang3.RandomStringUtils;
import org.apache.http.entity.ContentType;
import org.assertj.core.api.Assertions;
import org.infinispan.Infinispan;
import org.infinispan.Infinispans;
import org.infinispan.TestServer;
import org.infinispan.identities.Credentials;
import org.infinispan.util.CleanUpValidator;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

import cz.xtf.client.Http;
import cz.xtf.client.HttpResponseParser;
import cz.xtf.core.http.Https;
import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import cz.xtf.core.waiting.Waiters;
import cz.xtf.junit5.annotations.CleanBeforeAll;
import io.fabric8.kubernetes.api.model.Service;
import lombok.extern.slf4j.Slf4j;

/**
 * AdvancedSetupB compared to AdvancedSetupA tests endpoint encrypted by OpenShift.
 *
 * spec:
 *   autoscale:
 *     maxMemUsagePercent: 40
 *     maxReplicas: 5
 *     minMemUsagePercent: 20
 *     minReplicas: 3
 *   expose:
 *     type: Route
 *   service:
 *     type: Cache
 *     replicationFactor: 3
 *   replicas: 3
 */
@Slf4j
@CleanBeforeAll
class AdvancedSetupBIT {
   private static final OpenShift openShift = OpenShifts.master();
   private static final Infinispan infinispan = Infinispans.advancedSetupB();
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

      new CleanUpValidator(openShift, appName).withExposedRoute().withDefaultCredentials().withOpenShiftCerts().validate();
   }

   /**
    * Verify that default cache was created and is accessible through exposed LoadBalancer.
    * We need to trust all the certificates as used are valid only for OpenShifts internal communication.
    */
   @Test
   void defaultCacheAvailabilityTest() throws Exception {
      String keyUrl = "https://" + hostName + "/rest/v2/caches/default/availability-test";

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

   /**
    * Default AntiAffinity settings should prevent scheduling all three nodes on the same OCP host.
    * It's required to have OCP cluster with at least 3 worker nodes for the test to pass
    */
   @Test
   void antiAffinityTest() {
      List<Pod> clusterPods = openShift.pods().withLabel("clusterName", appName).list().getItems();
      Set<String> nodeNames = clusterPods.stream().map(p -> p.getSpec().getNodeName()).collect(Collectors.toSet());

      Assertions.assertThat(nodeNames).hasSize(3);
   }

   /**
    * Verifies replicationFactor of default cache is set to 1 by reading cache configuration.
    */
   @Test
   void replicationFactorTest() throws Exception {
      String request = "https://" + hostName + "/rest/v2/caches/default?action=config";
      String config = Http.get(request).basicAuth(user, pass).trustAll().execute().response();
      String numOwners = Stream.of(config.split(",")).filter(s -> s.contains("owners")).map(s -> s.trim().split(":")[1].trim()).findFirst().orElse("-1");

      Assertions.assertThat(numOwners).isEqualTo("3");
   }

   /**
    * Verifies autoscaling feature of default cache.
    */
   @Test
   void autoscalingTest() throws Exception {
      String request = "https://" + hostName + "/rest/v2/caches/default/autoscaling-key-";
      int i = 0;

      System.out.print("Loading");
      while (openShift.pods().withLabel("clusterName", appName).list().getItems().size() < 5) {
         i++;
         Http.put(request + i).basicAuth(user, pass).data(RandomStringUtils.randomAlphanumeric(1048576), ContentType.TEXT_PLAIN).trustAll().execute();
         System.out.print(".");
         Waiters.sleep(300);
      }
      System.out.println();
      System.out.println("Loading finished");

      // Wait for WellFormed. Cycle above exits moment last pod is created but not ready.
      infinispan.waitFor();

      System.out.print("Deleting");
      while (i > 1) {
         Http.delete(request + i).basicAuth(user, pass).trustAll().execute();
         i--;
         System.out.print(".");
         Waiters.sleep(300);
      }
      System.out.println();
      System.out.println("Deleting finished");

      BooleanSupplier bs = () -> openShift.pods().withLabel("clusterName", appName).list().getItems().size() == 3;
      new SimpleWaiter(bs, TimeUnit.MINUTES, 3, "Waiting for cluster stabilization").waitFor();
   }
}