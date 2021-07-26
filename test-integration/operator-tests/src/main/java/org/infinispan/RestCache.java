package org.infinispan;

import cz.xtf.client.Http;
import cz.xtf.client.HttpResponseParser;
import org.apache.http.entity.ContentType;

public class RestCache {
    private final String username;
    private final String password;

    private final String restUrl;

    public RestCache(String hostname, String username, String password) {
        this.username = username;
        this.password = password;

        this.restUrl = "https://" + hostname + "/rest/v2/caches/";
    }

    public void createCache(String cacheName, String cacheXML) throws Exception {
        Http.post(restUrl + cacheName).basicAuth(username, password).data(cacheXML, ContentType.APPLICATION_XML).trustAll().execute();
    }

    public void put(String cacheName, String key, String value) throws Exception {
        Http.put(restUrl + "/" + cacheName + "/" + key).basicAuth(username, password).data(value, ContentType.TEXT_PLAIN).trustAll().execute();
    }

    public String get(String cacheName, String key) throws Exception {
        HttpResponseParser parser = Http.get(restUrl + "/" + cacheName + "/" + key).basicAuth(username, password).trustAll().execute();
        return parser.response();
    }

    public void delete(String cacheName, String key) throws Exception {
        Http.delete(restUrl + "/" + cacheName + "/" + key).basicAuth(username, password).trustAll().execute();
    }
}
