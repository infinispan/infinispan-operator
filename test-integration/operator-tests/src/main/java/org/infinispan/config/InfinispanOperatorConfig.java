package org.infinispan.config;

import cz.xtf.core.config.XTFConfig;

public final class InfinispanOperatorConfig {
    public static final String NAME = "operator.infinispan.name";
    public static final String NAMESPACE = "operator.infinispan.namespace";
    public static final String CHANNEL = "operator.infinispan.channel";
    public static final String APPROVAL = "operator.infinispan.approval";
    public static final String SOURCE = "operator.infinispan.source";
    public static final String CSV = "operator.infinispan.version";

    public static String name() {
        return XTFConfig.get(NAME, "infinispan");
    }

    public static String namespace() {
        return XTFConfig.get(NAMESPACE);
    }

    public static String channel() {
        return XTFConfig.get(CHANNEL);
    }

    public static String approval() {
        return XTFConfig.get(APPROVAL);
    }

    public static String source() {
        return XTFConfig.get(SOURCE);
    }

    public static String version() {
        return XTFConfig.get(CSV);
    }
}
