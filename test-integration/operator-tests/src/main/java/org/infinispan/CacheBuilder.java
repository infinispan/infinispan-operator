package org.infinispan;

import lombok.extern.slf4j.Slf4j;
import org.dom4j.Document;
import org.dom4j.DocumentHelper;
import org.dom4j.Element;

@Slf4j
public class CacheBuilder {
   private final Document document;
   private final Element cache;

   public CacheBuilder(String name) {
      document = DocumentHelper.createDocument();

      cache = document
              .addElement("infinispan")
              .addElement("cache-container")
              .addElement("distributed-cache");

      cache.addAttribute("owners", "2")
              .addAttribute("mode", "SYNC")
              .addAttribute("name", name);
   }

   public CacheBuilder withEncoding() {
      Element encoding = cache.addElement("encoding");
      encoding.addElement("key").addAttribute("media-type", "text/plain");
      encoding.addElement("value").addAttribute("media-type", "text/plain");

      return this;
   }

   public CacheBuilder withTransaction() {
      cache.addElement("transaction").addAttribute("mode", "NONE");

      return this;
   }

   public CacheBuilder withMemory() {
      cache.addElement("memory").addElement("off-heap")
              .addAttribute("strategy", "REMOVE")
              .addAttribute("eviction", "MEMORY")
              .addAttribute("size", "96468992");

      return this;
   }

   public CacheBuilder withPartitionHandling() {
      cache.addElement("partition-handling").addAttribute("when-split","ALLOW_READ_WRITES").addAttribute("merge-policy", "REMOVE_ALL");

      return this;
   }

   public CacheBuilder withPersistence() {
      cache.addElement("persistence").addElement("file-store");

      return this;
   }

   public String build() {
      return document.asXML();
   }
}
