package org.infinispan.operator;

import java.io.IOException;
import java.util.List;
import java.util.stream.Collectors;
import java.util.stream.Stream;

import org.apache.http.entity.ContentType;
import org.assertj.core.api.Assertions;
import org.infinispan.Caches;
import org.infinispan.Infinispan;
import org.infinispan.Infinispans;
import org.infinispan.TestServer;
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
 * DevSetupIT tests deployment setup for development purposes with disabled security and monitoring features
 *
 * Check dev_setup.yaml Infinispan CR in test resources for input configuration.
 */
@Slf4j
@CleanBeforeAll
class DevSetupIT {
   private static final OpenShift openShift = OpenShifts.master();
   private static final Infinispan infinispan = Infinispans.devSetup();
   private static final TestServer testServer = TestServer.get();
   private static final String testServerHost = testServer.host();

   private static String appName;
   private static String hostName;

   @BeforeAll
   static void deployInfinispanCluster() throws IOException {
      appName =  infinispan.getClusterName();
      hostName = openShift.generateHostname(appName + "-external");

      infinispan.deploy();
      testServer.deploy();

      infinispan.waitFor();
      testServer.waitFor();

      Https.doesUrlReturnOK("http://" + testServerHost + "/ping").waitFor();
      Https.doesUrlReturnCode("http://" + hostName, 200).waitFor();
   }

   /**
    * Ensure that all resource created by Operator in this tests are deleted.
    */
   @AfterAll
   static void undeploy() throws IOException {
      infinispan.delete();

      new CleanUpValidator(openShift, appName).withExposedRoute().validate();
   }

   /**
    * Clustering should work out of the box in the OpenShift environment.
    */
   @Test
   void clusteringTest() throws Exception{
      // Create entry through REST
      String cacheUrl = "http://" + hostName + "/rest/v2/caches/cluster-test/";
      String keyUrl = cacheUrl + "cluster-test-key";

      Http.post(cacheUrl).data(Caches.fragile("cluster-test"), ContentType.APPLICATION_XML).execute();
      Http.put(keyUrl).data("cluster-test-value", ContentType.TEXT_PLAIN).execute();

      List<Pod> pods = openShift.pods().withLabel("clusterName", appName).list().getItems();
      List<String> ips = pods.stream().map(p -> p.getStatus().getPodIP()).collect(Collectors.toList());

      String getNode0 = String.format("http://" + testServerHost + "/hotrod/cluster?servicename=%s", ips.get(0));
      String getNode1 = String.format("http://" + testServerHost + "/hotrod/cluster?servicename=%s", ips.get(1));

      Assertions.assertThat(Http.get(getNode0).execute().code()).isEqualTo(200);
      Assertions.assertThat(Http.get(getNode1).execute().code()).isEqualTo(200);
   }

   /**
    * Verifies valid disabled authentication configuration for rest protocol.
    */
   @Test
   void restAuthTest() throws Exception {
      String cacheUrl = "http://" + hostName + "/rest/v2/caches/rest-noauth-test/";
      String entryUrl = cacheUrl + "rest-key";

      Http noAuthCachePut = Http.post(cacheUrl).data(Caches.fragile("rest-noauth-test"), ContentType.APPLICATION_XML);
      Http noAuthEntryPut = Http.post(entryUrl).data(Caches.fragile("rest-noauth-test"), ContentType.APPLICATION_XML);

      Assertions.assertThat(noAuthCachePut.execute().code()).isEqualTo(200);
      Assertions.assertThat(noAuthEntryPut.execute().code()).isEqualTo(204);
   }

   /**
    * Verifies disabled authentication configuration for hotrod protocol.
    */
   @Test
   void hotrodAuthTest() throws Exception {
      String noAuthGet = String.format("http://" + testServerHost + "/hotrod/auth?servicename=%s", appName);

      Assertions.assertThat(Http.get(noAuthGet).execute().code()).isEqualTo(200);
   }

   /**
    * ServiceMonitor object should not be created when org.infinispan/monitoring label is set to false
    */
   @Test
   void disableServiceMonitorTest() {
      Assertions.assertThat(openShift.monitoring().serviceMonitors().withName(appName + "monitor").get()).isEqualTo(null);
   }

   /**
    * Verify that default cache was created and is accessible.
    */
   @Test
   void defaultCacheAvailabilityTest() throws Exception {
      String keyUrl = "http://" + hostName + "/rest/v2/caches/default/availability-test";

      Http put = Http.put(keyUrl).data("default-cache-value", ContentType.TEXT_PLAIN);
      Http get = Http.get(keyUrl);

      Assertions.assertThat(put.execute().code()).isEqualTo(204);
      Assertions.assertThat(get.execute().response()).isEqualTo("default-cache-value");
   }

   /**
    * Default replication factor should be 2
    */
   @Test
   void defaultReplicationFactorTest() throws Exception {
      String request = "http://" + hostName + "/rest/v2/caches/default?action=config";
      String config = Http.get(request).execute().response();
      String[] owners = Stream.of(config.split(",")).filter(s -> s.contains("owners")).map(s -> s.trim().split(":")).findFirst().orElseThrow(() -> new IllegalStateException("Unable to retrieve owners"));

      Assertions.assertThat(owners[owners.length -1 ].replace("\"", "")).isEqualTo("2");
   }
}
