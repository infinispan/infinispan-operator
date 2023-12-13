package org.infinispan;

public class Caches {

   public static String testCache() {
      return fragile("testcache");
   }

   public static String fragile(String name) {
      return "<infinispan>\n" +
            "    <cache-container>\n" +
            "        <distributed-cache owners=\"1\" mode=\"SYNC\" name=\"" + name + "\">\n" +
            "            <encoding>\n" +
            "                <key media-type=\"text/plain\"/>\n" +
            "                <value media-type=\"text/plain\"/>\n" +
            "            </encoding>\n" +
            "            <transaction mode=\"NONE\"/>\n" +
            "            <memory storage=\"OFF_HEAP\" max-count=\"96468992\" when-full=\"REMOVE\"/>\n" +
            "            <partition-handling when-split=\"ALLOW_READ_WRITES\" merge-policy=\"REMOVE_ALL\"/>\n" +
            "        </distributed-cache>" +
            "    </cache-container>\n" +
            "</infinispan>\n";
   }
}
