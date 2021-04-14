import org.infinispan.client.hotrod.RemoteCacheManager;
import org.infinispan.commons.configuration.XMLStringConfiguration;
...

private void createCacheWithXMLConfiguration() {
    String cacheName = "CacheWithXMLConfiguration";
    String xml = String.format("<infinispan>" +
                                  "<cache-container>" +
                                  "<distributed-cache name=\"%s\" mode=\"SYNC\">" +
                                    "<encoding media-type=\"application/x-protostream\"/>" +
                                    "<locking isolation=\"READ_COMMITTED\"/>" +
                                    "<transaction mode=\"NON_XA\"/>" +
                                    "<expiration lifespan=\"60000\" interval=\"20000\"/>" +
                                  "</distributed-cache>" +
                                  "</cache-container>" +
                                "</infinispan>"
                                , cacheName);
    manager.administration().getOrCreateCache(cacheName, new XMLStringConfiguration(xml));
    System.out.println("Cache with configuration exists or is created.");
}
