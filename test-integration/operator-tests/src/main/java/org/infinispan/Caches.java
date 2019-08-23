package org.infinispan;

public class Caches {

   public static String testCache() {
      return "<infinispan>\n" +
            "    <cache-container>\n" +
            "        <distributed-cache name=\"testcache\" mode=\"SYNC\"/>\n" +
            "    </cache-container>\n" +
            "</infinispan>\n";
   }
}
