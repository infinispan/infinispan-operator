# Cross-Site Replication/Backup Configurations

There are two configuration approaches available for setting up cross-site replication between multiple **Infinispan** clusters.

1. Auto configuration managed by the **Infinispan Operator** via Kubernetes API communication between clusters.
1. Manual connection configurations provided via the `Infinispan CR`.

## Auto

### Pros

- Connection details for other sites are automatically discovered and configured by the **Infinispan Operator**.
- **Infinispan** clusters won't boot until connectivity has been established between each site and all of its backups.

### Cons

- The **Infinispan Operator** in each kubernetes/openshift cluster must be granted kubernetes API access to all of the backup clusters.
- Secrets must be generated and populated in each cluster for each of the backup clusters.
- All **Infinispan** clusters must be in the same matching project/namespace of their respective site.
- Won't function unless `host` intended for relay connection is specified on the backup site kubernetes `Service`.

### Example

The following `CRD` example is for the `LON` site/location with an additional (backup) site `NYC`:

```yaml
spec:
  ...
  service:
    type: DataGrid
    sites:
      local:
        name: LON
        expose:
          type: LoadBalancer
      locations:
      - name: LON
        url: openshift://api.site-a.devcluster.openshift.com:6443
        secretName: lon-token
      - name: NYC
        host: openshift://api.site-b.devcluster.openshift.com:6443
        secretName: nyc-token
  logging:
    categories:
      org.jgroups.protocols.TCP: error
      org.jgroups.protocols.relay.RELAY2: fatal
```

The `CRD` configuration for `NYC` will look the same, except for `NYC` substituted for `LON` in `spec.service.sites.local.name`.

Kubernetes API authentication between sites is achieved by the method indicated by the `url` `protocol`:

- `openshift` specifies that the secret referenced by `secretName` should contain a `ServiceAccount` token.
- `minikube` specifies that the secret referenced by `secretName` should contain the kubernetes CA cert, client cert, and client key.

These secrets must exist in every cluster except the one they belong to.

## Manual

### Pros

- No cross-cluster kubernetes API network access or authorization is required.
- The exposed `host` for the cross-site relay can be configured and managed in whatever manner is preferred, independent from the operator.
- Cross-site replication can be established between clusters running in a kubernetes/openshift environment and other clusters running in completely different types of infrastructure, such as VMs or bare-metal servers.
- Different clusters can be booted at different times, and the relay/bridge will be established once they are all up.

### Cons

- Additional network configuration is required to initiate cross-cluster replication.
- The exposed `host`/`port` must be planned/known ahead of time for each site.
- The **Infinispan Operator** will not validate cross-site connectivity, leaving it up to the developer to validate that the provided conection details were in fact correct.

### Example

The following `CRD` example is for the `NYC` site/location with an additional (backup) site `LON`:

```yaml
spec:
  ...
  service:
    type: DataGrid
    sites:
      local:
        name: NYC
        expose:
          type: LoadBalancer
      locations:
      - name: LON
        url: infinispan-lon.myhost.com
        port: 7900
      - name: NYC
        host: infinispan-nyc.myhost.com
        port: 7900
  logging:
    categories:
      org.jgroups.protocols.TCP: error
      org.jgroups.protocols.relay.RELAY2: fatal
```

The `port` option is optional, and defaults to `7900` if left out.

As with the auto option, the **Infinispan Operator** will use the `spec.service.local.expose.*` options to automatically create a site `Service` in the current cluster.
But it will simply assume that the backup site `host`/`port` values provided are valid.
