package org.infinispan.config;

import cz.xtf.core.config.XTFConfig;

public final class SuiteConfig {
    public static final String KEEP_RUNNING = "keep-running";
    public static final String OLM_RELEASE_CATALOG_NAME = "olm.catalog.ga";
    public static final String OLM_DEV_CATALOG_NAME = "olm.catalog.dev";
    public static final String OLM_OPERATORS_NAMESPACE = "olm.operators.namespace.global";
    public static final String OLM_OPERATORS_MARKETPLACE_NAMESPACE = "olm.operators.namespace.marketplace";

    public static String releaseCatalogName() {
        return XTFConfig.get(OLM_RELEASE_CATALOG_NAME, "community-operators");
    }

    public static String devCatalogName() {
        return XTFConfig.get(OLM_DEV_CATALOG_NAME, "infinispan-catalog");
    }

    public static String globalOperatorsNamespace() {
        return XTFConfig.get(OLM_OPERATORS_NAMESPACE, "operators");
    }

    public static String marketplaceNamespace() {
        return XTFConfig.get(OLM_OPERATORS_MARKETPLACE_NAMESPACE, "olm");
    }

    public static boolean keepRunning() {
        return Boolean.parseBoolean(XTFConfig.get(KEEP_RUNNING, "false"));
    }
}
