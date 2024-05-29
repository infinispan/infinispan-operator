package org.infinispan.operator.installation;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.dataformat.yaml.YAMLFactory;
import cz.xtf.core.openshift.*;
import cz.xtf.core.waiting.SimpleWaiter;
import io.fabric8.kubernetes.api.model.*;
import io.fabric8.kubernetes.api.model.rbac.*;
import io.fabric8.kubernetes.client.KubernetesClientException;
import io.fabric8.kubernetes.client.dsl.base.CustomResourceDefinitionContext;
import io.fabric8.openshift.api.model.operatorhub.v1.OperatorGroup;
import io.fabric8.openshift.api.model.operatorhub.v1.OperatorGroupBuilder;
import io.fabric8.openshift.api.model.operatorhub.v1alpha1.Subscription;
import org.infinispan.Infinispan;
import org.infinispan.Infinispans;
import org.infinispan.crd.GrafanaContextProvider;
import org.infinispan.crd.GrafanaDashboardContextProvider;
import org.infinispan.crd.GrafanaDataSourceContextProvider;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Tag;
import org.junit.jupiter.api.Test;

import java.io.File;
import java.io.IOException;
import java.util.*;
import java.util.function.BooleanSupplier;

/**
 * Tests Infinispan operator integration with Monitoring stack.
 * Installs Grafana Operator, deploys Grafana server and points it to Prometheus instance.
 *
 * Prerequisites: Enabled user workload monitoring.
 */
@Tag("unstable")
public class MonitoringStackIT {
    private static final CustomResourceDefinitionContext gcp = new GrafanaContextProvider().getContext();
    private static final CustomResourceDefinitionContext gdcp = new GrafanaDashboardContextProvider().getContext();
    private static final CustomResourceDefinitionContext gdscp = new GrafanaDataSourceContextProvider().getContext();

    private static final String grafanaNamespace = "grafana";
    private static final String operatorNamespace = "openshift-operators";

    private static final OpenShift clusterShift = OpenShifts.master();
    private static final OpenShift grafanaShift = OpenShifts.master(grafanaNamespace);
    private static final OpenShift operatorShift = OpenShifts.master(operatorNamespace);

    private static final OpenShiftWaiters grafanaWaiter = OpenShiftWaiters.get(grafanaShift, () -> false);

    private static final Infinispan infinispan = Infinispans.ocpCerts();

    @BeforeAll
    static void prepare() throws IOException {
        clusterShift.recreateProject();

        // Create Infinispan with ServiceMonitor
        infinispan.deploy();
        infinispan.waitFor();

        // Install Grafana Operator
        String grafanaSubscriptionPath = "src/test/resources/monitoring/grafana_sub.yaml";
        OperatorGroup operatorGroup = getOperatorGroup("grafana", grafanaNamespace);
        Subscription grafanaSubscription = clusterShift.operatorHub().subscriptions().load(new File(grafanaSubscriptionPath)).get();

        grafanaShift.recreateProject();
        grafanaShift.operatorHub().operatorGroups().create(operatorGroup);
        grafanaShift.operatorHub().subscriptions().create(grafanaSubscription);
        grafanaWaiter.areExactlyNPodsRunning(1, "control-plane", "controller-manager").waitFor();

        // Deploy Grafana server and Datasource
        String grafanaPath = "src/test/resources/monitoring/grafana.yaml";
        Map<String, Object> grafana = new ObjectMapper(new YAMLFactory()).readValue(new File(grafanaPath), Map.class);

        grafanaShift.customResource(gcp).create(grafanaNamespace, grafana);
        createClusterRoleBindingForGrafana();
        grafanaWaiter.areExactlyNPodsRunning(1, "app", "grafana").waitFor();

        Secret tokenSecret = grafanaShift.secrets().list().getItems().stream().filter(s -> s.getMetadata().getName().contains("grafana-serviceaccount-token")).findFirst().orElseThrow(() -> new IllegalStateException("Unable to retrieve service accounts token"));
        String token = new String(Base64.getDecoder().decode(tokenSecret.getData().get("token")));

        Map<String, Object> datasource = new ObjectMapper(new YAMLFactory()).readValue(getGrafanaDatasource(token), Map.class);
        grafanaShift.customResource(gdscp).create(grafanaNamespace, datasource);

        // Configure Infinispan Operator via ConfigMap and redeploy the Operator
        ConfigMap infinispanOperatorConfig = getInfinispanOperatorConfig(grafanaNamespace);
        operatorShift.configMaps().createOrReplace(infinispanOperatorConfig);
    }

    static void createClusterRoleBindingForGrafana() {
        Subject subject = new SubjectBuilder()
                .withKind("ServiceAccount")
                .withName("grafana-serviceaccount")
                .withNamespace("grafana").build();

        ClusterRoleBinding crb = new ClusterRoleBindingBuilder()
                .withNewMetadata().withName("grafana-monitor").endMetadata()
                .withNewRoleRef().withKind("ClusterRole").withName("cluster-monitoring-view").endRoleRef()
                .build();
        crb.getSubjects().add(subject);
        grafanaShift.rbac().clusterRoleBindings().createOrReplace(crb);
    }

    @AfterAll
    static void undeploy() throws IOException {
        infinispan.delete();
        grafanaShift.projects().withName(grafanaNamespace).delete();
        operatorShift.configMaps().withName("infinispan-operator-config").delete();
        clusterShift.events().delete();
    }

    /**
     * Checks that Grafana Dashboard gets created upon Operator restart as all the conditions are fulfilled.
     */
    @Test
    void dashboardAvailabilityTest() {
        // Wait 3 minutes for Grafana Dashboard to appear or fail
        BooleanSupplier bs = () -> {
            try {
                grafanaShift.customResource(gdcp).get(grafanaNamespace, "infinispan");
                return true;
            } catch (KubernetesClientException e) {
                return false;
            }
        };
        new SimpleWaiter(bs).waitFor();
    }

    private static OperatorGroup getOperatorGroup(String name, String namespace) {
        OperatorGroupBuilder ogb = new OperatorGroupBuilder();
        ogb.withNewMetadata().withName(name).endMetadata();
        ogb.withNewSpec().addNewTargetNamespace(namespace).endSpec();

        return ogb.build();
    }

    private static String getGrafanaDatasource(String token) {
        return
                "apiVersion: integreatly.org/v1alpha1\n" +
                "kind: GrafanaDataSource\n" +
                "metadata:\n" +
                "  name: grafanadatasource\n" +
                "spec:\n" +
                "  name: prometheus.yaml\n" +
                "  datasources:\n" +
                "  - name: Prometheus\n" +
                "    type: prometheus\n" +
                "    access: proxy\n" +
                "    url: https://thanos-querier.openshift-monitoring.svc.cluster.local:9091\n" +
                "    isDefault: true\n" +
                "    editable: true\n" +
                "    jsonData:\n" +
                "      httpHeaderName1: Authorization\n" +
                "      tlsSkipVerify: true\n" +
                "      timeInterval: \"5s\"\n" +
                "    secureJsonData:\n" +
                "      httpHeaderValue1: 'Bearer " + token + "'";
    }

    private static ConfigMap getInfinispanOperatorConfig(String grafanaNamespace) {
        Map<String, String> config = new HashMap<>();
        config.put("grafana.dashboard.namespace", grafanaNamespace);
        config.put("grafana.dashboard.name", "infinispan");
        config.put("grafana.dashboard.monitoring.key", "middleware");

        ConfigMapBuilder cmp = new ConfigMapBuilder();
        cmp.withNewMetadata().withName("infinispan-operator-config").endMetadata();
        cmp.addToData(config);

        return cmp.build();
    }
}