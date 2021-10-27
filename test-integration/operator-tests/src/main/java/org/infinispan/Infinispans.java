package org.infinispan;

public class Infinispans {

   public static Infinispan cacheService() {
      return new Infinispan("src/test/resources/infinispans/cache_service.yaml");
   }

   public static Infinispan customLibs() {
      return new Infinispan("src/test/resources/infinispans/custom_libs.yaml");
   }

   public static Infinispan dataGridService() {
      return new Infinispan("src/test/resources/infinispans/datagrid_service.yaml");
   }

   public static Infinispan devSetup() {
      return new Infinispan("src/test/resources/infinispans/dev_setup.yaml");
   }

   public static Infinispan clientTlsValidation() {
      return new Infinispan("src/test/resources/infinispans/client_validation.yaml");
   }

   public static Infinispan clientTlsAuthentication() {
      return new Infinispan("src/test/resources/infinispans/client_authentication.yaml");
   }
}
