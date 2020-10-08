package org.infinispan;

import java.io.File;
import java.io.IOException;
import java.util.Base64;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import java.util.function.BooleanSupplier;

import org.infinispan.cr.InfinispanObject;
import org.infinispan.cr.Status;
import org.infinispan.cr.status.Condition;
import org.infinispan.crd.InfinispanContextProvider;
import org.infinispan.identities.Credentials;
import org.infinispan.identities.Identities;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.dataformat.yaml.YAMLFactory;

import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import cz.xtf.core.waiting.SimpleWaiter;
import io.fabric8.kubernetes.client.dsl.base.CustomResourceDefinitionContext;
import lombok.Getter;

@Getter
public class Infinispan {
   private static final OpenShift openShift = OpenShifts.master();
   private static final CustomResourceDefinitionContext crdc = new InfinispanContextProvider().getContext();

   private final String clusterName;
   private final String crPath;

   private InfinispanObject infinispanObject;
   private Map<String, Object> infinispan;

   public Infinispan(String crPath) {
      try {
         infinispan = new ObjectMapper(new YAMLFactory()).readValue(new File(crPath), Map.class);
         infinispanObject = new ObjectMapper().convertValue(infinispan, InfinispanObject.class);
      } catch (IOException e) {
         throw new IllegalStateException("Unable to load Infinispan from: " + crPath, e);
      }

      this.clusterName = infinispanObject.getMetadata().getName();
      this.crPath = crPath;
   }

   public void deploy() throws IOException {
      openShift.customResource(crdc).create(openShift.getNamespace(), infinispan);
   }

   public void delete() throws IOException {
      openShift.customResource(crdc).delete(openShift.getNamespace(), clusterName);
   }

   public void sync() {
      infinispan = openShift.customResource(crdc).get(openShift.getNamespace(), clusterName);
      infinispanObject = new ObjectMapper().convertValue(infinispan, InfinispanObject.class);
   }

   public void waitFor() {
      openShift.waiters().areExactlyNPodsReady(infinispanObject.getSpec().getReplicas(), "clusterName", clusterName).timeout(TimeUnit.MINUTES, 5).waitFor();

      BooleanSupplier bs = () -> {
         sync();

         List<Condition> conditions = infinispanObject.getStatus().getConditions();
         if (conditions != null) {
            Condition wellFormed = conditions.stream().filter(c -> "wellFormed".equals(c.getType())).findFirst().orElse(null);
            return wellFormed != null && "True".equals(wellFormed.getStatus());
         } else {
            return false;
         }
      };

      new SimpleWaiter(bs).timeout(TimeUnit.MINUTES, 3).waitFor();
   }

   public Credentials getDefaultCredentials() throws IOException {
      return getCredentials(clusterName + "-generated-secret", "developer");
   }

   public Credentials getCredentials(String username) throws IOException {
      return getCredentials(infinispanObject.getSpec().getSecurity().getEndpointSecretName() ,username);
   }

   private Credentials getCredentials(String secretName, String username) throws IOException {
      Map<String, String> creds = openShift.getSecret(secretName).getData();
      String identitiesYaml = new String(Base64.getDecoder().decode(creds.get("identities.yaml")));
      Identities identities = new ObjectMapper(new YAMLFactory()).readValue(identitiesYaml, Identities.class);
      return identities.getCredentials(username);
   }
}
