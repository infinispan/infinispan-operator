# Backup a cluster and restore

1. Create source cluster:

```yaml
apiVersion: infinispan.org/v1
kind: Infinispan
metadata:
  name: source-cluster
spec:
  replicas: 2
  service:
    type: DataGrid
```

2. Populate cluster content

3. Backup all cluster content

```yaml
apiVersion: infinispan.org/v2alpha1
kind: Backup
metadata:
  name: example-backup
spec:
  cluster: source-cluster

```

4. Create target cluster
```yaml
apiVersion: infinispan.org/v1
kind: Infinispan
metadata:
  name: target-cluster
spec:
  replicas: 2
  service:
    type: DataGrid
```

5. Restore content on target cluster
```yaml
apiVersion: infinispan.org/v2alpha1
kind: Restore
metadata:
  name: example-restore
spec:
  backup: example-backup
  cluster: target-cluster
```
