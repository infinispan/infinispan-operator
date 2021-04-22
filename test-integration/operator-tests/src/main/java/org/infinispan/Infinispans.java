package org.infinispan;

public class Infinispans {

   public static Infinispan defaultAuth() {
      return new Infinispan("src/test/resources/infinispans/cr_minimal.yaml");
   }

   public static Infinispan customAuth() {
      return new Infinispan("src/test/resources/infinispans/cr_minimal_with_auth.yaml");
   }

   public static Infinispan advancedSetupA() {
      return new Infinispan("src/test/resources/infinispans/advanced_setup_a.yaml");
   }

   public static Infinispan advancedSetupB() {
      return new Infinispan("src/test/resources/infinispans/advanced_setup_b.yaml");
   }

   public static Infinispan customLibs() {
      return new Infinispan("src/test/resources/infinispans/custom_libs.yaml");
   }

   public static Infinispan minimalSetup() {
      return new Infinispan("src/test/resources/infinispans/minimal_setup.yaml");
   }
}
