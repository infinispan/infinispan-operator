package org.infinispan.junit;

import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import lombok.extern.slf4j.Slf4j;
import org.infinispan.config.SuiteConfig;
import org.infinispan.crd.InfinispanContextProvider;
import org.junit.jupiter.api.extension.AfterAllCallback;
import org.junit.jupiter.api.extension.BeforeAllCallback;
import org.junit.jupiter.api.extension.ExtensionContext;

@Slf4j
public class CleanUpCallback implements BeforeAllCallback, AfterAllCallback {
    private static final OpenShift openShift = OpenShifts.master();

    @Override
    public void beforeAll(ExtensionContext context) {
        cleanUp();
    }

    @Override
    public void afterAll(ExtensionContext context) {
        if(!SuiteConfig.keepRunning()) {
            cleanUp();
        }
    }

    private void cleanUp() {
        log.info("Removing test resources...");
        openShift.customResource(new InfinispanContextProvider().getContext()).delete();
        openShift.secrets().withLabel("suite.infinispan.org/clean", "true").delete();
    }
}
