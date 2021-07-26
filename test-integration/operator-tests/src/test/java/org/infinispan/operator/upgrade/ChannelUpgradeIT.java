package org.infinispan.operator.upgrade;

import lombok.extern.slf4j.Slf4j;
import org.assertj.core.api.Assertions;
import org.infinispan.*;
import org.infinispan.config.SuiteConfig;
import org.infinispan.identities.Credentials;
import org.infinispan.junit.CleanUp;
import org.infinispan.junit.OperatorCleanUp;
import org.infinispan.util.PackageManifestsParser;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

/**
 * Instantiates latest version in previous channel and verifies the upgrade
 */
@Slf4j
@CleanUp
@OperatorCleanUp
public class ChannelUpgradeIT {
    private static final Infinispan infinispan = Infinispans.datagrid();

    private static InfinispanOperator operator;

    private static String sourceChannel;
    private static String targetChannel;
    private static String targetCSVVersion;

    /**
     * Install released version.
     */
    @BeforeAll
    static void prepare() throws Exception {
        PackageManifestsParser devPMParser = PackageManifestsParser.init(SuiteConfig.devCatalogName());

        targetCSVVersion = devPMParser.getCurrentCSV();
        targetChannel = devPMParser.getLatestChannel();
        sourceChannel = devPMParser.getPreviouslyReleasedChannel();

        log.info("Channel upgrade will be performed from '{}' to '{}'", sourceChannel, targetChannel);
        log.info("Expecting '{}' to be the final version of the upgrade", targetCSVVersion);

        operator = new InfinispanOperator(sourceChannel, "Manual");

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

        // Trigger the upgrade
        operator.changeChannel(targetChannel);

        while(!operator.getCurrentCSV().equals(targetCSVVersion)) {
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
        }
    }
}
