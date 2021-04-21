package org.infinispan;

public class Infinispans {

   public static Infinispan advancedSetupA() {
      return new Infinispan("src/test/resources/infinispans/advanced_setup_a.yaml");
   }

   public static Infinispan advancedSetupB() {
      return new Infinispan("src/test/resources/infinispans/advanced_setup_b.yaml");
   }

   public static Infinispan minimalSetup() {
      return new Infinispan("src/test/resources/infinispans/minimal_setup.yaml");
   }
}
