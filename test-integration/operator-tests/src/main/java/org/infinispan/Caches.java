package org.infinispan;

public class Caches {

   public static String testCache() {
      return fragile("testcache");
   }

   public static String fragile(String name) {
      return "<infinispan>\n" +
            "    <cache-container>\n" +
            "        <distributed-cache owners=\"1\" mode=\"SYNC\" name=\"" + name + "\">\n" +
            "            <transaction mode=\"NONE\"/>\n" +
            "            <memory>\n" +
            "                <off-heap strategy=\"REMOVE\" eviction=\"MEMORY\" size=\"96468992\"/>\n" +
            "            </memory>\n" +
            "            <partition-handling when-split=\"ALLOW_READ_WRITES\" merge-policy=\"REMOVE_ALL\"/>\n" +
            "        </distributed-cache>" +
            "    </cache-container>\n" +
            "</infinispan>\n";
   }
}
