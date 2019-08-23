package org.infinispan.operator.security;

import java.io.IOException;

import org.apache.http.entity.ContentType;
import org.infinispan.Caches;
import org.infinispan.Infinispan;
import org.infinispan.Infinispans;
import org.infinispan.identities.Credentials;
import org.junit.jupiter.api.BeforeAll;

import cz.xtf.builder.builders.RouteBuilder;
import cz.xtf.client.Http;
import cz.xtf.core.http.Https;
import cz.xtf.junit5.annotations.CleanBeforeAll;
import io.fabric8.openshift.api.model.Route;
import lombok.extern.slf4j.Slf4j;

@Slf4j
@CleanBeforeAll
class DefaultAuthTest extends AuthTest {
   private static Infinispan infinispan = Infinispans.defaultAuth();

   @BeforeAll
   static void deployInfinispanCluster() throws IOException {
      testServer.deploy();
      infinispan.deploy();

      appName =  infinispan.getClusterName();
      hostName = openShift.generateHostname(appName);

      Route route = new RouteBuilder(appName).forService(appName).targetPort(11222).exposedAsHost(hostName).build();
      openShift.createRoute(route);

      testServer.waitFor();
      infinispan.waitFor();

      Credentials developer = infinispan.getDefaultCredentials();
      user = developer.getUsername();
      pass = developer.getPassword();

      Https.doesUrlReturnOK("http://" + testServerHost + "/ping").waitFor();
      Https.doesUrlReturnCode("http://" + hostName, 401).waitFor();

      Http.post("http://" + hostName + "/rest/v2/caches/testcache").basicAuth(user, pass).data(Caches.testCache(), ContentType.APPLICATION_XML).execute().code();
   }
}
