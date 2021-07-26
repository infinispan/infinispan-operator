package org.infinispan;

public class Caches {

   public static String testCache() {
      return fragile("testcache");
   }

   public static String fragile(String name) {
      CacheBuilder cacheBuilder = new CacheBuilder(name);
      cacheBuilder.withEncoding();
      cacheBuilder.withTransaction();
      cacheBuilder.withMemory();
      cacheBuilder.withPartitionHandling();

      return cacheBuilder.build();
   }

   public static String persisted(String name) {
      CacheBuilder cacheBuilder = new CacheBuilder(name);
      cacheBuilder.withEncoding();
      cacheBuilder.withTransaction();
      cacheBuilder.withMemory();
      cacheBuilder.withPersistence();
      cacheBuilder.withPartitionHandling();

      return cacheBuilder.build();
   }
}
