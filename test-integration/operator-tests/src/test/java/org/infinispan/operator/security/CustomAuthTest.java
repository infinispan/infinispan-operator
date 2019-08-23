package org.infinispan.operator.security;

import java.io.IOException;
import java.nio.file.Paths;

import org.apache.http.entity.ContentType;
import org.infinispan.Caches;
import org.infinispan.Infinispan;
import org.infinispan.Infinispans;
import org.infinispan.identities.Credentials;
import org.junit.jupiter.api.BeforeAll;

import cz.xtf.builder.builders.RouteBuilder;
import cz.xtf.builder.builders.SecretBuilder;
import cz.xtf.client.Http;
import cz.xtf.core.http.Https;
import cz.xtf.junit5.annotations.CleanBeforeAll;
import io.fabric8.kubernetes.api.model.Secret;
import io.fabric8.openshift.api.model.Route;
import lombok.extern.slf4j.Slf4j;

@Slf4j
@CleanBeforeAll
class CustomAuthTest extends AuthTest {
   private static final Infinispan infinispan = Infinispans.customAuth();

   @BeforeAll
   static void deploy() throws IOException {
      testServer.deploy();
      infinispan.deploy();

      appName = infinispan.getClusterName();
      hostName = openShift.generateHostname(appName);

      Secret secret = new SecretBuilder("connect-secret").addData("identities.yaml", Paths.get("src/test/resources/secrets/identities.yaml")).build();
      Route route = new RouteBuilder(appName).forService(appName).targetPort(11222).exposedAsHost(hostName).build();

      openShift.secrets().create(secret);
      openShift.createRoute(route);

      testServer.waitFor();
      infinispan.waitFor();

      Credentials developer = infinispan.getCredentials("testuser");
      user = developer.getUsername();
      pass = developer.getPassword();

      log.info("Username: {}", user);
      log.info("Password: {}", pass);

      Https.doesUrlReturnCode("http://" + hostName, 401).waitFor();

      Http.post("http://" + hostName + "/rest/v2/caches/testcache").basicAuth(user, pass).data(Caches.testCache(), ContentType.APPLICATION_XML).execute().code();
   }
}
