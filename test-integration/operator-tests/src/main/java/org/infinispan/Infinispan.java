package org.infinispan;

import java.io.File;
import java.io.IOException;
import java.util.Base64;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import java.util.function.BooleanSupplier;
import java.util.stream.Collectors;

import org.infinispan.cr.InfinispanObject;
import org.infinispan.cr.status.Condition;
import org.infinispan.crd.InfinispanContextProvider;
import org.infinispan.identities.Credentials;
import org.infinispan.identities.Identities;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.dataformat.yaml.YAMLFactory;

import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import cz.xtf.core.waiting.SimpleWaiter;
import io.fabric8.kubernetes.api.model.Event;
import io.fabric8.kubernetes.api.model.ObjectReference;
import io.fabric8.kubernetes.client.dsl.base.CustomResourceDefinitionContext;
import lombok.Getter;
import lombok.extern.slf4j.Slf4j;

@Getter
@Slf4j
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
      BooleanSupplier bs = () -> {
         sync();

         if (infinispanObject.getStatus() != null) {
            List<Condition> conditions = infinispanObject.getStatus().getConditions();
            Condition wellFormed = conditions.stream().filter(c -> "WellFormed".equals(c.getType())).findFirst().orElse(null);
            return wellFormed != null && "True".equals(wellFormed.getStatus());
         } else {
            return false;
         }
      };

      new SimpleWaiter(bs).timeout(TimeUnit.MINUTES, 10).onTimeout(() -> onTimeout()).waitFor();

      Map<String, String> labels = new HashMap<>();
      labels.put("clusterName", clusterName);
      labels.put("app", "infinispan-pod");

      if(openShift.getLabeledPods(labels).size() != infinispanObject.getSpec().getReplicas()) {
         throw new IllegalStateException(clusterName + " is WellFormed but cluster pod count doesn't match expected replicas!");
      }
   }

   private void onTimeout() {
      List<Event> events = openShift.events().list().getItems();
      List<Event> podEvents = events.stream().filter(e -> {
         ObjectReference or = e.getInvolvedObject();
         return or.getKind().equals("Pod") && or.getName().startsWith(clusterName);
      }).collect(Collectors.toList());
      List<Event> ssEvents = events.stream().filter(e -> {
         ObjectReference or = e.getInvolvedObject();
         return or.getKind().equals("StatefulSet") && or.getName().equals(clusterName);
      }).collect(Collectors.toList());
      List<Event> ispnEvents = events.stream().filter(e -> {
         ObjectReference or = e.getInvolvedObject();
         return or.getKind().equals("Infinispan") && or.getName().equals(clusterName);
      }).collect(Collectors.toList());

      log.info("--- Pod events ---");
      podEvents.forEach(e -> log.info(e.getMessage()));

      log.info("--- StatefulSet events ---");
      ssEvents.forEach(e -> log.info(e.getMessage()));

      log.info("--- Infinispan events ---");
      ispnEvents.forEach(e -> log.info(e.getMessage()));
   }

   public Credentials getDefaultCredentials() throws IOException {
      return getCredentials(clusterName + "-generated-secret", "developer");
   }

   public Credentials getOperatorCredentials() throws IOException {
      return getCredentials(clusterName + "-generated-operator-secret", "operator");
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
