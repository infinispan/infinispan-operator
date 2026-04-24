# Testing Instructions

## Test Frameworks

The Infinispan Operator uses two levels of testing:

| Type           | Framework                  | Location                                          | Run Command       |
|----------------|----------------------------|---------------------------------------------------|--------------------|
| Unit tests     | Ginkgo + Gomega + envtest  | `api/**/*_test.go`, `controllers/*_test.go`, `pkg/**/*_test.go` | `make test`        |
| E2E tests      | Ginkgo + Gomega            | `test/e2e/`                                       | `make <cr>-test`   |

## Writing Unit Tests

### Framework

Unit tests use the standard Go testing package with Ginkgo (BDD-style) and Gomega (assertions). The `envtest` package provides a local Kubernetes API server for controller and webhook tests.

### Location

Unit tests are colocated with source files following Go convention:
- `api/v1/infinispan_webhook_test.go` — webhook validation tests
- `api/v1/types_util_test.go` — type utility tests
- `api/v2alpha1/*_test.go` — v2alpha1 API tests
- `controllers/*_test.go` — controller unit tests
- `pkg/**/*_test.go` — package-level tests

### Example: Webhook Test

```go
var _ = Describe("Infinispan Webhooks", func() {
    Context("when validating an Infinispan CR", func() {
        It("should reject invalid replicas", func() {
            ispn := &v1.Infinispan{
                Spec: v1.InfinispanSpec{
                    Replicas: -1,
                },
            }
            _, err := ispn.ValidateCreate()
            Expect(err).To(HaveOccurred())
        })
    })
})
```

### Example: Standard Go Test

```go
func TestMyFunction(t *testing.T) {
    result := MyFunction("input")
    assert.Equal(t, "expected", result)
}
```

### Mocking

- Mocks are generated using `mockgen` (via `make generate-mocks`).
- Pipeline interface mocks live in `pkg/reconcile/pipeline/infinispan/api_mocks.go`.
- After modifying interfaces in `api.go`, regenerate mocks with `make generate-mocks`.

## Writing E2E Tests

### Directory Structure

E2E tests are organized by Custom Resource type under `test/e2e/`:
- `test/e2e/infinispan/` — Infinispan CR tests
- `test/e2e/cache/` — Cache CR tests
- `test/e2e/batch/` — Batch CR tests
- `test/e2e/schema/` — Schema CR tests
- `test/e2e/backup-restore/` — Backup/Restore CR tests
- `test/e2e/xsite/` — Cross-site replication tests
- `test/e2e/webhook/` — OLM webhook integration tests
- `test/e2e/upgrade/` — OLM upgrade tests
- `test/e2e/hotrod-rolling-upgrade/` — Hot Rod rolling upgrade tests
- `test/e2e/multinamespace/` — Multi-namespace tests
- `test/e2e/utils/` — Shared test utilities and helpers

### Prerequisites

E2E tests run against a real Kubernetes cluster:
- A running Kubernetes or OpenShift cluster (configured via `KUBECONFIG`)
- The operator CRDs installed (`make install`)
- The `WATCH_NAMESPACE` environment variable set

### Running E2E Tests

```bash
# Run Infinispan CR E2E tests
make infinispan-test

# Run Cache CR E2E tests
make cache-test

# Run Batch CR E2E tests
make batch-test

# Run Schema CR E2E tests
make schema-test

# Run Backup/Restore E2E tests
make backuprestore-test

# Run cross-site E2E tests (45 min timeout)
make xsite-test

# Run OLM webhook E2E tests
make webhook-test

# Run OLM upgrade E2E tests (2h timeout)
make upgrade-test

# Run tests via script directly with custom timeout and parallelism
scripts/run-tests.sh <test-bundle> [timeout] [parallel-count]
```

## Running Tests

```bash
# Unit tests (uses envtest for K8s API server)
make test

# Lint before committing
make lint

# Format before committing
make fmt

# Vet before committing
make vet
```
