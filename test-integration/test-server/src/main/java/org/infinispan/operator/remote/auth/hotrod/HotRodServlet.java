package org.infinispan.operator.remote.auth.hotrod;

import java.io.IOException;
import java.io.PrintWriter;

import jakarta.servlet.annotation.WebServlet;
import jakarta.servlet.http.HttpServlet;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;

import org.infinispan.client.hotrod.RemoteCache;
import org.infinispan.client.hotrod.RemoteCacheManager;
import org.infinispan.client.hotrod.configuration.ConfigurationBuilder;
import org.infinispan.client.hotrod.exceptions.HotRodClientException;
import org.infinispan.configuration.cache.CacheMode;
import org.infinispan.configuration.cache.Configuration;

/**
 * Used by MinimalSetupIT with unencrypted endpoints.
 */
@WebServlet("/hotrod/auth")
public class HotRodServlet extends HttpServlet {
   private static final long serialVersionUID = 1L;

   protected void doGet(HttpServletRequest request, HttpServletResponse response) throws IOException {
      PrintWriter pw = new PrintWriter(response.getOutputStream());

      try {
         String serviceName = request.getParameter("servicename");
         String namespace = request.getParameter("namespace");
         String username = request.getParameter("username");
         String password = request.getParameter("password");
         String encrypted = request.getParameter("encrypted");

         ConfigurationBuilder builder = new ConfigurationBuilder();
         builder.addServer().host(serviceName).port(11222);

         if(username != null || password != null) {
            builder.security().authentication().realm("default").serverName("infinispan").username(username).password(password).enable();
         }

         if(encrypted != null) {
            builder.security().ssl().sniHostName(serviceName + "." + namespace + ".svc")
                    .trustStoreFileName("/etc/" + serviceName + "-cert-secret/tls.crt")
                    .trustStoreType("PEM");
         }

          Configuration c = new org.infinispan.configuration.cache.ConfigurationBuilder()
                  .clustering()
                  .cacheMode(CacheMode.DIST_SYNC)
                  .hash()
                  .numOwners(2)
                  .build();
         RemoteCacheManager rcm = new RemoteCacheManager(builder.build());
         RemoteCache<String, String> rc = rcm.administration().getOrCreateCache("hotrod-auth-test", c);

         rc.put("authorized-hotrod-key", "secret-value");

         if(!"secret-value".equals(rc.get("authorized-hotrod-key"))) {
            response.setStatus(HttpServletResponse.SC_UNAUTHORIZED);
            pw.println("UNAUTHORIZED");
         }
      } catch (HotRodClientException e) {
         response.setStatus(HttpServletResponse.SC_UNAUTHORIZED);
         pw.println("UNAUTHORIZED: " + e.getMessage());
      } catch (Exception e) {
         response.setStatus(HttpServletResponse.SC_INTERNAL_SERVER_ERROR);
         pw.println("INTERNAL SERVER ERROR: " + e.getMessage());
      } finally {
         pw.close();
      }
   }
}
