package org.infinispan.util;

import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import io.fabric8.openshift.api.model.operatorhub.lifecyclemanager.v1.PackageChannel;
import io.fabric8.openshift.api.model.operatorhub.lifecyclemanager.v1.PackageManifest;
import org.infinispan.config.InfinispanOperatorConfig;

import java.util.List;
import java.util.stream.Collectors;

public class PackageManifestsParser {
    private static final OpenShift openShift = OpenShifts.master();

    private final PackageManifest pm;
    private final List<String> channels;

    public static PackageManifestsParser init(String catalogName) {
        String operatorName = InfinispanOperatorConfig.name();
        PackageManifest packageManifest = openShift.operatorHub().packageManifests().list().getItems()
                .stream().filter(pm ->
                        operatorName.equals(pm.getMetadata().getName()) &&
                        catalogName.equals(pm.getStatus().getCatalogSource()))
                .findFirst().orElseThrow(
                        () -> new IllegalStateException("Unable retrieve " + operatorName + " in " + catalogName)
                );

        return new PackageManifestsParser(packageManifest);
    }

    private PackageManifestsParser(PackageManifest pm) {
        this.pm = pm;
        this.channels = pm.getStatus().getChannels().stream().map(PackageChannel::getName)
                .filter(ch -> ch.split("\\.").length == 3).sorted(new ChannelComparator()).collect(Collectors.toList());
    }

    public String getLatestChannel() {
        return channels.get(channels.size() - 1);
    }

    public String getPreviouslyReleasedChannel() {
        return channels.get(channels.size() - 2);
    }

    public String getCurrentCSV() {
        return pm.getStatus().getChannels().stream().filter(ch -> ch.getName().equals(getLatestChannel())).findFirst()
                .orElseThrow(() -> new IllegalStateException("Unable retrieve CurrentCSV for " + getLatestChannel() + "channel")).getCurrentCSV();
    }
}
