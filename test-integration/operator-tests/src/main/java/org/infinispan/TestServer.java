package org.infinispan;

import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.Arrays;
import java.util.Collections;

import org.apache.maven.it.VerificationException;
import org.apache.maven.it.Verifier;

import cz.xtf.builder.builders.ApplicationBuilder;
import cz.xtf.core.bm.BinarySourceBuild;
import cz.xtf.core.bm.BuildManager;
import cz.xtf.core.bm.BuildManagers;
import cz.xtf.core.bm.ManagedBuild;
import cz.xtf.core.bm.ManagedBuildReference;
import cz.xtf.core.http.Https;
import cz.xtf.core.image.Image;
import cz.xtf.core.openshift.OpenShift;
import cz.xtf.core.openshift.OpenShifts;
import lombok.Getter;

public class TestServer {
   private static final OpenShift openShift = OpenShifts.master();
   private static final BuildManager bm = BuildManagers.get();
   private static final Image builderImage = Image.get("testserver");

   @Getter
   private static final String name = "test-server";

   private static Path deploymentPath;

   public static TestServer get() {
      return new TestServer();
   }

   private ManagedBuild build;
   private ApplicationBuilder testApp;

   private TestServer() {
      build = build();
      testApp = resources();
   }

   public TestServer withSecret(String secretName) {
      testApp.deploymentConfig().podTemplate().addSecretVolume(secretName, secretName).container().addVolumeMount(secretName, "/etc/" + secretName, true);

      return this;
   }

   public void deploy() {
      bm.deploy(build);
      testApp.buildApplication().deploy();
   }

   public void delete() {
      openShift.deploymentConfigs().withLabel("app", name).delete();
      openShift.services().withLabel("app", name).delete();
      openShift.routes().withLabel("app", name).delete();
   }

   public String host() {
      return openShift.generateHostname(name);
   }

   public void waitFor() {
      openShift.waiters().isDcReady(name).waitFor();
      Https.doesUrlReturnOK("http://" + host() + "/ping").waitFor();
   }

   private ApplicationBuilder resources() {
      ManagedBuildReference mbr = bm.getBuildReference(build);

      ApplicationBuilder appBuilder = new ApplicationBuilder(name);
      appBuilder.deploymentConfig().addLabel("app", name).onConfigurationChange().onImageChange();
      appBuilder.deploymentConfig().podTemplate().container().fromImage(mbr.getNamespace(), mbr.getStreamName()).port(8080);
      appBuilder.service().addLabel("app", name).port(8080);
      appBuilder.route().addLabel("app", name);

      return appBuilder;
   }

   private ManagedBuild build() {
      return new BinarySourceBuild(builderImage.getUrl(), buildLocaly(), Collections.emptyMap(), name);
   }

   private Path buildLocaly() {
      if (deploymentPath == null) {
         try {
            Verifier maven = new Verifier("../test-server");

            maven.setAutoclean(true);
            maven.executeGoals(Arrays.asList("package", "-DskipTests"));
            maven.resetStreams();

            deploymentPath = Paths.get("../test-server/target/build");
         } catch (VerificationException e) {
            throw new IllegalStateException("Failed to build test project locally", e);
         }
      }

      return deploymentPath;
   }
}
