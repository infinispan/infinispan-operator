package org.infinispan.operator.security;

import java.io.IOException;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;

import org.apache.http.entity.ContentType;
import org.assertj.core.api.Assertions;
import org.infinispan.TestServer;
import org.junit.jupiter.api.Test;

import cz.xtf.client.Http;
import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;

abstract class AuthTest {
   static final OpenShift openShift = OpenShifts.master();

   static final TestServer testServer = TestServer.get();
   static final String testServerHost = testServer.host();

   static String appName;
   static String hostName;

   static String user;
   static String pass;

   @Test
   void restCorrectCredentials() throws IOException {
      String putUrl = "http://" + hostName + "/rest/testcache/correct";
      String getUrl = "http://" + hostName + "/rest/testcache/correct";

      int putCode = Http.put(putUrl).basicAuth(user, pass).data("credentials", ContentType.TEXT_PLAIN).execute().code();
      int getCode = Http.get(getUrl).basicAuth(user, pass).execute().code();

      Assertions.assertThat(putCode).isEqualTo(204);
      Assertions.assertThat(getCode).isEqualTo(200);
   }

   @Test
   void restInvalidPassword() throws IOException {
      String putUrl = "http://" + hostName + "/rest/testcache/invalid";

      int putCode = Http.put(putUrl).basicAuth(user, "invalid").data("credentials", ContentType.TEXT_PLAIN).execute().code();

      Assertions.assertThat(putCode).isEqualTo(401);
   }

   @Test
   void restInvalidUser() throws IOException {
      String putUrl = "http://" + hostName + "/rest/testcache/incorrect";

      int putCode = Http.put(putUrl).basicAuth("notauser", pass).data("user", ContentType.TEXT_PLAIN).execute().code();

      Assertions.assertThat(putCode).isEqualTo(401);
   }

   @Test
   void restNoCredentials() throws IOException {
      String putUrl = "http://" + hostName + "/rest/testcache/no";

      int putCode = Http.put(putUrl).data("credentials", ContentType.TEXT_PLAIN).execute().code();

      Assertions.assertThat(putCode).isEqualTo(401);
   }

   @Test
   void hotrodCorrectCredentials() throws IOException {
      String encodedPass = URLEncoder.encode(pass, StandardCharsets.UTF_8.toString());
      String get = String.format("http://" + testServerHost + "/hotrod/auth?username=%s&password=%s&servicename=%s", user, encodedPass, appName);

      Assertions.assertThat(Http.get(get).execute().code()).isEqualTo(200);
   }

   @Test
   void hotrodInvalidPassword() throws IOException {
      String get = String.format("http://" + testServerHost + "/hotrod/auth?username=%s&password=%s&servicename=%s", user, "invalid", appName);

      Assertions.assertThat(Http.get(get).execute().code()).isEqualTo(401);
   }

   @Test
   void hotrodInvalidUser() throws IOException {
      String encodedPass = URLEncoder.encode(pass, StandardCharsets.UTF_8.toString());
      String get = String.format("http://" + testServerHost + "/hotrod/auth?username=%s&password=%s&servicename=%s", "notauser", encodedPass, appName);

      Assertions.assertThat(Http.get(get).execute().code()).isEqualTo(401);
   }
}
