package org.infinispan.operator.upgrade;

import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import cz.xtf.core.waiting.Waiters;
import lombok.extern.slf4j.Slf4j;
import org.assertj.core.api.Assertions;
import org.infinispan.*;
import org.infinispan.config.SuiteConfig;
import org.infinispan.identities.Credentials;
import org.infinispan.junit.CleanUp;
import org.infinispan.junit.OperatorCleanUp;
import org.infinispan.util.PackageManifestsParser;
import org.junit.jupiter.api.Assumptions;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

import java.util.concurrent.TimeUnit;

/**
 * Instantiates the latest release in current channel and verifies the upgrade.
 */
@Slf4j
@CleanUp
@OperatorCleanUp
public class MicroUpgradeIT {
    private static final OpenShift openShift = OpenShifts.master();
    private static final Infinispan infinispan = Infinispans.datagrid();

    private static InfinispanOperator operator;

    /**
     * Install released version.
     */
    @BeforeAll
    static void prepare() throws Exception {
        PackageManifestsParser gaPMParser = PackageManifestsParser.init(SuiteConfig.releaseCatalogName());
        PackageManifestsParser devPMParser = PackageManifestsParser.init(SuiteConfig.devCatalogName());

        String targetCSVVersion = devPMParser.getCurrentCSV();
        String sourceCSVVersion = gaPMParser.getCurrentCSV();
        String sourceChannel = gaPMParser.getLatestChannel();

        // There is no micro version to upgrade from
        Assumptions.assumeFalse(targetCSVVersion.endsWith(".0"), targetCSVVersion + " is the first version of the channel");

        log.info("Version upgrade will be performed from '{}' to '{}'", sourceCSVVersion, targetCSVVersion);

        operator = new InfinispanOperator(sourceChannel, "Manual", sourceCSVVersion);

        operator.subscribe();

        operator.isUpgradeReadyWaiter().waitFor();
        operator.approveInstallPlan();
        operator.hasInstallCompletedWaiter().waitFor();

        infinispan.deploy();
        infinispan.waitFor();
    }

    /**
     * Trigger manual upgrade to dev version.
     */
    @Test
    void upgradeTest() throws Exception {
        int clusterSize = infinispan.getSize();

        Credentials credentials = infinispan.getDefaultCredentials();
        String user = credentials.getUsername();
        String pass = credentials.getPassword();

        // Create persisted cache, put some data
        String hostname = infinispan.getHostname();
        String cacheName = "update-cache";

        RestCache rest = new RestCache(hostname, user, pass);

        rest.createCache(cacheName, Caches.persisted(cacheName));

        rest.put(cacheName, "test-key-read", "read");
        rest.put(cacheName, "test-key-update", "update");
        rest.put(cacheName, "test-key-delete", "delete");

        operator.isUpgradeReadyWaiter().waitFor();
        operator.approveInstallPlan();
        operator.hasInstallCompletedWaiter().waitFor();

        // Wait for Infinispan to upgrade
        String serverImage = operator.getServerImage();
        log.info("Expecting the cluster to start with: {}", serverImage);
        infinispan.isClusterRunningWithServerImageWaiter(serverImage, clusterSize).waitFor();

        // Verify the cache and the data
        Assertions.assertThat(rest.get(cacheName, "test-key-read")).isEqualTo("read");
        Assertions.assertThat(rest.get(cacheName, "test-key-update")).isEqualTo("update");
        Assertions.assertThat(rest.get(cacheName, "test-key-delete")).isEqualTo("delete");

        // Modify the data
        rest.put(cacheName, "test-key-update", "updated");
        rest.delete(cacheName, "test-key-delete");

        // Verify the data can be read correctly on restart
        openShift.pods().withLabel("clusterName", infinispan.getClusterName()).delete();

        Waiters.sleep(TimeUnit.SECONDS, 30);
        infinispan.waitFor();

        Assertions.assertThat(rest.get(cacheName, "test-key-read")).isEqualTo("read");
        Assertions.assertThat(rest.get(cacheName, "test-key-update")).isEqualTo("updated");
        Assertions.assertThat(rest.get(cacheName, "test-key-delete")).isEqualTo("");
    }
}
