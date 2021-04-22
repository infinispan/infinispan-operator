package com.redhat.xpaas.jdg.listener;

import org.infinispan.filter.NamedFactory;
import org.infinispan.metadata.Metadata;
import org.infinispan.notifications.cachelistener.filter.CacheEventFilter;
import org.infinispan.notifications.cachelistener.filter.CacheEventFilterFactory;
import org.infinispan.notifications.cachelistener.filter.EventType;

import java.io.Serializable;

@NamedFactory(name = "basic-filter-factory")
public class BasicKeyValueFilterFactory implements CacheEventFilterFactory {

	@Override
	public CacheEventFilter<String, String> getFilter(final Object[] params) {
		return new BasicKeyValueFilter();
	}

	static class BasicKeyValueFilter implements CacheEventFilter<String, String>, Serializable {
		@Override
		public boolean accept(String key, String oldValue, Metadata oldMetadata, String newValue, Metadata newMetadata, EventType eventType) {
			return "key-0".equals(key);
		}
	}
}