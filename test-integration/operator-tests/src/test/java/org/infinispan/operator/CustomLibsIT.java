package org.infinispan.operator;

import cz.xtf.core.config.XTFConfig;
import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShiftWaiters;
import cz.xtf.core.openshift.OpenShifts;
import cz.xtf.junit5.annotations.CleanBeforeAll;
import io.fabric8.kubernetes.api.model.*;
import org.apache.maven.it.VerificationException;
import org.apache.maven.it.Verifier;
import org.assertj.core.api.Assertions;
import org.infinispan.Infinispan;
import org.infinispan.Infinispans;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

import java.io.File;
import java.io.IOException;
import java.nio.file.Paths;
import java.util.Collections;

@CleanBeforeAll
public class CustomLibsIT {
    private static final OpenShift openShift = OpenShifts.master();
    private static final OpenShiftWaiters openShiftWaiters = OpenShiftWaiters.get(openShift, () -> false);
    private static final Infinispan infinispan = Infinispans.customLibs();

    @BeforeAll
    static void prepare() throws IOException {
        preparePVC();
        preparePod();

        openShiftWaiters.areExactlyNPodsRunning(1, "app", "infinispan-libs").waitFor();

        buildLibs();
        uploadLibs();
        deletePod();

        infinispan.deploy();
        infinispan.waitFor();
    }

    @AfterAll
    static void undeploy() throws Exception {
        infinispan.delete();

        openShift.persistentVolumeClaims().withLabel("app", "infinispan-libs").delete();
        openShift.events().delete();
    }

    @Test
    void test() {
        Pod pod = openShift.getPod("custom-libs-0");
        String logs = openShift.getPodLog(pod);

        Assertions.assertThat(logs).contains("Loaded extension 'basic-filter-factory'");
    }

    private static void preparePVC() {
        PersistentVolumeClaimBuilder pvc = new PersistentVolumeClaimBuilder();
        pvc.withNewMetadata().withName("infinispan-libs").addToLabels("app", "infinispan-libs").endMetadata();
        pvc.withNewSpec()
                .withAccessModes("ReadWriteOnce")
                .withNewResources().withRequests(Collections.singletonMap("storage", new Quantity("100Mi")))
                .endResources().endSpec();

        openShift.createPersistentVolumeClaim(pvc.build());
    }

    private static void preparePod() {
        String serverImage = XTFConfig.get("org.infinispan.image.server", "quay.io/infinispan/server:latest");

        VolumeBuilder vb = new VolumeBuilder();
        vb.withName("lib-pv-storage").withNewPersistentVolumeClaim("infinispan-libs", false);

        VolumeMountBuilder vmb = new VolumeMountBuilder();
        vmb.withMountPath("/tmp/libs");
        vmb.withName("lib-pv-storage");

        SecurityContextBuilder scb = new SecurityContextBuilder();
        scb.withAllowPrivilegeEscalation(false);
        scb.withCapabilities(new CapabilitiesBuilder().withDrop("ALL").build());
        scb.withRunAsNonRoot(true);
        scb.withSeccompProfile(new SeccompProfileBuilder().withType("RuntimeDefault").build());

        ContainerBuilder cb = new ContainerBuilder();
        cb.withName("lib-pv-container")
                .withImage(serverImage)
                .withVolumeMounts(vmb.build());
        cb.withSecurityContext(scb.build());

        PodBuilder pod = new PodBuilder();
        pod.withNewMetadata()
                .withName("infinispan-libs")
                .withLabels(Collections.singletonMap("app", "infinispan-libs"))
                .endMetadata();
        pod.withNewSpec()
                .withNewSecurityContext().withFsGroup(2000L).endSecurityContext()
                .withVolumes(vb.build())
                .withContainers(cb.build())
                .endSpec();

        openShift.createPod(pod.build());
    }

    private static void buildLibs() {
        try {
            String projectPath = Paths.get("src/test/resources/libs/custom-filter").toAbsolutePath().toString();
            Verifier maven = new Verifier(projectPath);

            maven.setAutoclean(true);
            maven.executeGoals(Collections.singletonList("package"));
            maven.resetStreams();
        } catch (VerificationException e) {
            throw new IllegalStateException("Failed to build test project locally", e);
        }
    }

    private static void uploadLibs() {
        File file = new File("src/test/resources/libs/custom-filter/target/custom-filter-1.0.jar");

        openShift.pods().withName("infinispan-libs").file("/tmp/libs/custom-filter-1.0.jar").upload(file.toPath());
    }

    private static void deletePod() {
        openShift.pods().withLabel("app", "infinispan-libs").delete();
    }
}
