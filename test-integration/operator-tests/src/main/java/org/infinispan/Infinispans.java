package org.infinispan;

public class Infinispans {

   public static Infinispan cache() {
      return new Infinispan("src/test/resources/infinispans/cache.yaml");
   }

   public static Infinispan datagrid() {
      return new Infinispan("src/test/resources/infinispans/datagrid.yaml");
   }

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
}
