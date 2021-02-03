package org.infinispan.operator.remote.cluster;

import java.io.IOException;
import java.io.PrintWriter;

import javax.servlet.annotation.WebServlet;
import javax.servlet.http.HttpServlet;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

import org.infinispan.client.hotrod.RemoteCache;
import org.infinispan.client.hotrod.RemoteCacheManager;
import org.infinispan.client.hotrod.configuration.ClientIntelligence;
import org.infinispan.client.hotrod.configuration.ConfigurationBuilder;
import org.infinispan.client.hotrod.exceptions.HotRodClientException;

/**
 * Used by MinimalSetupIT with unencrypted endpoints.
 */
@WebServlet("/hotrod/cluster")
public class HotRodCluster extends HttpServlet {
   private static final long serialVersionUID = 2L;

   protected void doGet(HttpServletRequest request, HttpServletResponse response) throws IOException {
      PrintWriter pw = new PrintWriter(response.getOutputStream());

      try {
         String serviceName = request.getParameter("servicename");
         String username = request.getParameter("username");
         String password = request.getParameter("password");

         ConfigurationBuilder builder = new ConfigurationBuilder();
         builder.addServer().host(serviceName).port(11222);
         builder.security().authentication().realm("default").serverName("infinispan").username(username).password(password).enable();
         builder.clientIntelligence(ClientIntelligence.BASIC);

         RemoteCacheManager rcm = new RemoteCacheManager(builder.build());
         RemoteCache<String, String> rc = rcm.getCache("cluster-test");

         if(!"cluster-test-value".equals(rc.get("cluster-test-key"))) {
            response.setStatus(HttpServletResponse.SC_NOT_FOUND);
            pw.println("NOT FOUND");
         }
      } catch (Exception e) {
         response.setStatus(HttpServletResponse.SC_INTERNAL_SERVER_ERROR);
         pw.println("INTERNAL SERVER ERROR: " + e.getMessage());
      } finally {
         pw.close();
      }
   }
}
