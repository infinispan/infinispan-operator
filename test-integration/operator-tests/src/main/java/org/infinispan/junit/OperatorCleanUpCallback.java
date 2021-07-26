package org.infinispan.junit;

import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import io.fabric8.openshift.api.model.operatorhub.v1alpha1.Subscription;
import lombok.extern.slf4j.Slf4j;
import org.infinispan.config.InfinispanOperatorConfig;
import org.infinispan.config.SuiteConfig;
import org.junit.jupiter.api.extension.AfterAllCallback;
import org.junit.jupiter.api.extension.BeforeAllCallback;
import org.junit.jupiter.api.extension.ExtensionContext;

@Slf4j
public class OperatorCleanUpCallback implements BeforeAllCallback, AfterAllCallback {
    private static final OpenShift openShift = OpenShifts.master(SuiteConfig.globalOperatorsNamespace());

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
        Subscription sub = openShift.operatorHub().subscriptions().withName(InfinispanOperatorConfig.name()).get();

        if(sub != null) {
            String csvName = sub.getStatus().getInstalledCSV();

            log.info("Removing {} Operator from the cluster...", csvName);
            openShift.operatorHub().subscriptions().withName(InfinispanOperatorConfig.name()).delete();

            if (csvName != null) {
                openShift.operatorHub().clusterServiceVersions().withName(csvName).delete();
            }
        }
    }
}
