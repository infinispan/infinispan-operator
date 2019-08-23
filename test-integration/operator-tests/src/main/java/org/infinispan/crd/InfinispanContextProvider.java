package org.infinispan.crd;

import cz.xtf.core.openshift.crd.CustomResourceDefinitionContextProvider;
import io.fabric8.kubernetes.client.dsl.base.CustomResourceDefinitionContext;

public class InfinispanContextProvider implements CustomResourceDefinitionContextProvider {
   private CustomResourceDefinitionContext infinispanContext;

   public CustomResourceDefinitionContext getContext() {
      if (infinispanContext == null) {
         infinispanContext = new CustomResourceDefinitionContext.Builder()
               .withGroup("infinispan.org")
               .withPlural("infinispans")
               .withScope("Namespaced")
               .withVersion("v1")
               .build();
      }

      return infinispanContext;
   }
}
