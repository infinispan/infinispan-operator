package org.infinispan.operator.remote.encryption;

import java.io.IOException;
import java.io.PrintWriter;

import javax.servlet.annotation.WebServlet;
import javax.servlet.http.HttpServlet;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

import org.infinispan.client.hotrod.RemoteCache;
import org.infinispan.client.hotrod.RemoteCacheManager;
import org.infinispan.client.hotrod.configuration.ConfigurationBuilder;

@WebServlet("/hotrod/encryption-provided")
public class EncryptionProvidedHotRod extends HttpServlet {
   private static final long serialVersionUID = 3L;

   protected void doGet(HttpServletRequest request, HttpServletResponse response) throws IOException {
      PrintWriter pw = new PrintWriter(response.getOutputStream());

      try {
         String serviceName = request.getParameter("servicename");
         String username = request.getParameter("username");
         String password = request.getParameter("password");

         ConfigurationBuilder builder = new ConfigurationBuilder();
         builder.addServer().host(serviceName).port(11222);
         builder.security().authentication().realm("default").serverName("infinispan").username(username).password(password).enable();
         builder.security().ssl().trustStorePath("/etc/" + serviceName + "-cert-secret/tls.crt");

         RemoteCacheManager rcm = new RemoteCacheManager(builder.build());
         RemoteCache<String, String> rc = rcm.administration().getOrCreateCache("hotrod-auth-test", "org.infinispan.DIST_SYNC");

         rc.put(username, password);

         if(!password.equals(rc.get(username))) {
            response.setStatus(HttpServletResponse.SC_NOT_FOUND);
            pw.println("NOT_FOUND");
         }
      } catch (Exception e) {
         response.setStatus(HttpServletResponse.SC_INTERNAL_SERVER_ERROR);
         pw.println("INTERNAL_SERVER_ERROR: " + e.getMessage() );
      } finally {
         pw.close();
      }
   }
}
