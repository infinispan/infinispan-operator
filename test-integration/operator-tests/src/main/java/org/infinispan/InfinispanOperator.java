package org.infinispan;

import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import cz.xtf.core.waiting.SimpleWaiter;
import cz.xtf.core.waiting.Waiter;
import io.fabric8.kubernetes.api.model.EnvVar;
import io.fabric8.openshift.api.model.operatorhub.v1alpha1.*;
import lombok.extern.slf4j.Slf4j;
import org.infinispan.config.InfinispanOperatorConfig;
import org.infinispan.config.SuiteConfig;

import java.util.List;
import java.util.concurrent.TimeUnit;
import java.util.function.BooleanSupplier;

@Slf4j
public class InfinispanOperator {
    private final OpenShift openShift;

    private final String name;
    private final String channel;
    private final String installPlanApproval;
    private final String source;
    private final String sourceNamespace;

    private String startingCSV;
    private String currentCSV;
    private String currentInstallPlan;

    public InfinispanOperator(String channel, String approval) {
        this.openShift = OpenShifts.master(SuiteConfig.globalOperatorsNamespace());

        this.name = InfinispanOperatorConfig.name();
        this.channel = channel;
        this.installPlanApproval = approval;
        this.source = SuiteConfig.devCatalogName();
        this.sourceNamespace = SuiteConfig.marketplaceNamespace();
    }

    public InfinispanOperator(String channel, String approval, String startingCSV) {
        this.openShift = OpenShifts.master(SuiteConfig.globalOperatorsNamespace());

        this.name = InfinispanOperatorConfig.name();
        this.channel = channel;
        this.installPlanApproval = approval;
        this.source = SuiteConfig.devCatalogName();
        this.sourceNamespace = SuiteConfig.marketplaceNamespace();
        this.startingCSV = startingCSV;
    }

    /**
     * Subscribes the Operator
     */
    public void subscribe() {
        SubscriptionBuilder subBuilder = new SubscriptionBuilder();
        subBuilder.withNewMetadata().withName(name).endMetadata();
        subBuilder.withNewSpec()
                .withChannel(channel)
                .withInstallPlanApproval(installPlanApproval)
                .withName(name)
                .withSource(source)
                .withSourceNamespace(sourceNamespace)
                .endSpec();

        if(startingCSV != null) {
            subBuilder.editSpec().withStartingCSV(startingCSV).endSpec();
        }

        openShift.operatorHub().subscriptions().create(subBuilder.build());
    }

    public boolean isUpgradeReady() {
        return "UpgradePending".equals(openShift.operatorHub().subscriptions().withName(name).get().getStatus().getState());
    }

    public Waiter isUpgradeReadyWaiter() {
        return new SimpleWaiter(this::isUpgradeReady).timeout(TimeUnit.MINUTES,10);
    }

    public Waiter hasInstallCompletedWaiter() {
        BooleanSupplier bs = () -> {
            InstallPlan ip = openShift.operatorHub().installPlans().withName(currentInstallPlan).get();
            return "Complete".equals(ip.getStatus().getPhase());
        };

        return new SimpleWaiter(bs).reason("Installing CSV " + currentCSV + "...").logPoint(Waiter.LogPoint.BOTH);
    }

    /**
     * Approve pending InstallPlan when installPlanApproval is set to Manual.
     */
    public void approveInstallPlan() {
        Subscription sub = openShift.operatorHub().subscriptions().withName(name).get();
        currentInstallPlan = sub.getStatus().getInstallplan().getName();

        InstallPlan installPlan = openShift.operatorHub().installPlans().withName(currentInstallPlan).edit(
                ip -> new InstallPlanBuilder(ip).editSpec().withApproved(true).endSpec().build()
        );

        currentCSV = installPlan.getSpec().getClusterServiceVersionNames().get(0);
        log.info("Current CSV version is set to: {}", currentCSV);
    }

    public void changeChannel(String targetChannel) {
        openShift.operatorHub().subscriptions().withName(name).edit(
                sub -> new SubscriptionBuilder(sub).editSpec().withChannel(targetChannel).endSpec().build()
        );
    }

    public void changeCatalog(String targetCatalog) {
        openShift.operatorHub().subscriptions().withName(name).edit(
                sub -> new SubscriptionBuilder(sub).editSpec().withSource(targetCatalog).endSpec().build()
        );
    }

    public ClusterServiceVersion getCSV() {
        return openShift.operatorHub().clusterServiceVersions().withName(currentCSV).get();
    }

    public String getCurrentCSV() {
        return currentCSV;
    }

    public String getServerImage() {
        List<EnvVar> envVars = getCSV().getSpec().getInstall().getSpec().getDeployments().get(0).getSpec().getTemplate().getSpec().getContainers().get(0).getEnv();
        return envVars.stream().filter(e -> "RELATED_IMAGE_OPENJDK".equals(e.getName())).findFirst().orElseThrow(() -> new IllegalStateException("Unable to retreive current server image")).getValue();
    }

    /**
     * Remove Operator installation
     */
    public void unsubscribe() {
        Subscription sub = openShift.operatorHub().subscriptions().withName("datagrid").get();
        String csvName = sub.getStatus().getInstalledCSV();

        openShift.getOpenshiftUrl().toString();
        openShift.operatorHub().subscriptions().withName("datagrid").delete();
        openShift.operatorHub().clusterServiceVersions().withName(csvName).delete();
    }
}