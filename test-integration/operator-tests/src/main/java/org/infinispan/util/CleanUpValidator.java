package org.infinispan.util;

import java.util.ArrayList;
import java.util.List;
import java.util.function.BooleanSupplier;

import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.waiting.SimpleWaiter;

public class CleanUpValidator {
   private final OpenShift openShift;
   private final String appName;

   private List<BooleanSupplier> conditions;

   public CleanUpValidator(OpenShift openShift, String appName) {
      this.openShift = openShift;
      this.appName = appName;

      conditions = new ArrayList<>();
      conditions.add(
            () -> openShift.apps().statefulSets().withName(appName).get() == null &&
                  openShift.secrets().withName(appName + "-generated-operator-secret").get() == null &&
                  openShift.services().withName(appName).get() == null &&
                  openShift.services().withName(appName + "-ping").get() == null &&
                  openShift.configMaps().withName(appName + "-configuration").get() == null &&
                  openShift.pods().withLabel("clusterName", appName).list().getItems().isEmpty() &&
                  openShift.persistentVolumeClaims().withLabel("clusterName", appName).list().getItems().isEmpty()
      );
   }

   public CleanUpValidator withExposedRoute() {
      conditions.add(() -> openShift.routes().withName(appName + "-external").get() == null);
      return this;
   }

   public CleanUpValidator withExposedLoadBalancer() {
      conditions.add(() -> openShift.services().withName(appName + "-external").get() == null);
      return this;
   }

   public CleanUpValidator withDefaultCredentials() {
      conditions.add(() -> openShift.secrets().withName(appName + "-generated-secret").get() == null);
      return this;
   }

   public CleanUpValidator withOpenShiftCerts() {
      conditions.add(() -> openShift.secrets().withName(appName + "-cert-secret").get() == null);
      return this;
   }

   public CleanUpValidator withServiceMonitor() {
      conditions.add(() -> openShift.monitoring().serviceMonitors().withName(appName + "monitor").get() == null);
      return this;
   }

   public void validate() {
      new SimpleWaiter(() -> conditions.stream().allMatch(BooleanSupplier::getAsBoolean), "Waiting for resource deletion").waitFor();
   }
}
