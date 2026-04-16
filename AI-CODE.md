# Coding Instructions

## Tech Stack
* **Go Version:** 1.25 (see `go.mod`)
* **Build Tool:** Make
* **Framework:** Operator SDK / kubebuilder v3
* **Key Libraries:**
  - `sigs.k8s.io/controller-runtime` — core operator framework (reconciler, manager, client)
  - `k8s.io/api`, `k8s.io/client-go` — Kubernetes client libraries
  - `github.com/onsi/ginkgo` + `github.com/onsi/gomega` — BDD testing
  - `go.uber.org/zap` — structured logging
  - `github.com/openshift/api` — OpenShift-specific APIs (Routes)
  - `github.com/prometheus-operator/prometheus-operator` — Prometheus monitoring integration

## Project Architecture

* **`main.go`** — Entry point. Selects between `operator` and `listener` subcommands.
* **`api/v1/`** — Infinispan CRD type definitions, webhooks, and utilities.
* **`api/v2alpha1/`** — Backup, Restore, Cache, Batch, Schema CRD types and webhooks.
* **`controllers/`** — Reconciler implementations for each CRD.
* **`controllers/constants/`** — Shared controller constants.
* **`controllers/resources/`** — Kubernetes resource builders.
* **`pkg/reconcile/pipeline/infinispan/`** — Pipeline-based reconciliation architecture.
  - `api.go` — Pipeline interfaces (Handler, Context, FlowStatus).
  - `handler/provision/` — Provision phase: ConfigMaps, Secrets, dependencies.
  - `handler/configure/` — Configure phase: auth, TLS, server configuration.
  - `handler/manage/` — Manage phase: upgrades, scaling, StatefulSet updates.
  - `context/` — Pipeline context implementation.
  - `pipeline/` — Pipeline orchestration.
* **`pkg/infinispan/`** — Infinispan client (REST/Hot Rod) and version management.
* **`pkg/kubernetes/`** — Kubernetes utility helpers.
* **`pkg/http/`** — HTTP client utilities.
* **`pkg/templates/`** — Go template management.
* **`launcher/`** — Operator and listener launcher packages.
* **`config/`** — Kustomize-managed Kubernetes manifests (CRDs, RBAC, webhooks, manager deployment, samples).
* **`test/e2e/`** — End-to-end tests (Ginkgo-based).
* **`test-integration/`** — Java/Maven integration tests.
* **`documentation/`** — AsciiDoc documentation.
* **`scripts/`** — CI and utility scripts.
* **`hack/`** — Development helper scripts.

## Custom Resources

| Kind       | API Version   | Description                              |
|------------|---------------|------------------------------------------|
| Infinispan | v1            | Main CRD — manages Infinispan clusters   |
| Cache      | v2alpha1      | Manages individual caches                |
| Backup     | v2alpha1      | Manages cluster backups                  |
| Restore    | v2alpha1      | Manages cluster restores                 |
| Batch      | v2alpha1      | Executes batch operations                |
| Schema     | v2alpha1      | Manages Protobuf schemas                 |

All CRDs have validating and defaulting webhooks.

## Common Build Commands
* **Build operator binary:** `make manager`
* **Run unit tests:** `make test`
* **Lint:** `make lint`
* **Format:** `make fmt`
* **Vet:** `make vet`
* **Generate CRDs/RBAC/webhooks:** `make manifests`
* **Generate deepcopy methods:** `make generate`
* **Generate mocks:** `make generate-mocks`
* **Build operator image:** `make operator-build IMG=<image>`
* **Install CRDs into cluster:** `make install`
* **Run operator locally:** `make run`
* **Run E2E tests:** `make infinispan-test`, `make cache-test`, `make batch-test`, `make schema-test`, `make backuprestore-test`, `make xsite-test`, `make webhook-test`, `make upgrade-test`

## Development Standards

### CRD and API Changes
* CRD types live in `api/v1/` and `api/v2alpha1/`. After modifying types, always run `make manifests` and `make generate` to regenerate CRD YAML and deepcopy methods.
* Use kubebuilder markers for validation, defaults, and documentation (e.g., `// +kubebuilder:validation:Minimum=0`).
* Webhooks for defaulting and validation are in `*_webhook.go` files alongside the types.
* CRD changes must be backward-compatible with existing Custom Resources in live clusters.

### Controller Patterns
* The main Infinispan reconciler uses a **pipeline architecture**: handlers chained in three phases (Provision, Configure, Manage).
* Other reconcilers (Cache, Backup, Restore, Batch, Schema) use simpler direct reconciliation.
* Controllers use Kubernetes ownership semantics (`OwnerReference`) for garbage collection.
* Use the `EventRecorder` to log important events to the Kubernetes cluster.

### Code Style
* **Formatting:** `go fmt` (via `make fmt`).
* **Linting:** golangci-lint with `errorlint` and `bodyclose` linters (via `make lint`).
* **Vet:** `go vet` (via `make vet`).
* Run `make fmt` and `make vet` before committing.

### Commit Messages
* Commit messages must start with `[#00000] Summary` where `00000` is the GitHub issue number.

### Git Branches
* Branches should be named `issueid/issue_summary` and use `origin/main` as the upstream.

## Development Platform
* **Issues:** Use GitHub issues. No issue templates are currently defined — use plain issues with a descriptive title and body.

## Related Projects
* **Server:** The Infinispan server source code is in `../infinispan`
* **Console:** The Infinispan Console source code is in `../infinispan-console`
* **Website:** The Infinispan website source code is in `../infinispan.github.io`
