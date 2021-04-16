import org.infinispan.client.hotrod.DefaultTemplate;
import org.infinispan.client.hotrod.RemoteCache;
import org.infinispan.client.hotrod.RemoteCacheManager;
...

      builder.remoteCache("my-cache")
             .templateName(DefaultTemplate.DIST_SYNC);
      builder.remoteCache("another-cache")
             .configuration("<infinispan><cache-container><distributed-cache name=\"another-cache\"><encoding media-type=\"application/x-protostream\"/></distributed-cache></cache-container></infinispan>");
      try (RemoteCacheManager cacheManager = new RemoteCacheManager(builder.build())) {
      // Get a remote cache that does not exist.
      // Rather than return null, create the cache from a template.
      RemoteCache<String, String> cache = cacheManager.getCache("my-cache");
      // Store a value.
      cache.put("hello", "world");
      // Retrieve the value and print it.
      System.out.printf("key = %s\n", cache.get("hello"));
