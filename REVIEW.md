# Pull Request Reviewing Instructions

## Verify the basics

- Commit messages reference a GitHub issue in the `[#00000] Summary` format.
- Commits are squashed into self-contained units of work.
- Tests are included or a convincing justification is provided.

## What Important means here

Reserve Important for findings that would break behavior, cause data loss, or block a rollback: incorrect reconciliation
logic, CRD schema changes that break existing Custom Resources, RBAC permission gaps, missing error handling for
Kubernetes API calls, and security issues (leaked secrets, overly broad RBAC). Style, naming, and refactoring
suggestions are Nit at most.

## Operator-specific concerns

Watch for these domain-specific issues that general review might miss:
- **CRD backward compatibility:** changes to CRD types (`api/v1/`, `api/v2alpha1/`) must not break existing Custom Resources in live clusters. Field removals, type changes, and required field additions are breaking changes.
- **Generated code:** after API type changes, `make manifests` and `make generate` must be run. Verify that `zz_generated.deepcopy.go` and CRD YAML in `config/crd/bases/` are up to date.
- **RBAC permissions:** changes to the resources a controller watches or modifies may require RBAC marker updates (`// +kubebuilder:rbac:...`) and regeneration of `config/rbac/`.
- **Reconciliation idempotency:** reconcilers must be idempotent — running the same reconciliation twice with no external changes must produce the same result. Watch for side effects that are not guarded by state checks.
- **Error handling:** Kubernetes API calls can fail transiently. Verify that errors are returned (triggering a requeue) rather than silently swallowed.
- **Status updates:** changes to status subresources should use `Status().Update()`, not `Update()`, to avoid conflicts with spec changes.
- **Ownership and garbage collection:** resources created by the operator should have `OwnerReference` set so they are cleaned up when the parent CR is deleted.
- **Webhook validation:** defaulting and validating webhooks must handle both create and update operations correctly. Validation should not block legitimate updates.
- **Pipeline handler ordering:** the Infinispan reconciler uses a pipeline with three phases (Provision, Configure, Manage). New handlers must be placed in the correct phase and position.

## Cap the nits

Report at most five Nits per review. If you found more, say "plus N similar items" in the summary instead of posting
them inline. If everything you found is a Nit, lead the summary with "No blocking issues."

## Do not report

- Anything CI already enforces: lint, formatting, vet errors
- Test-only code that intentionally violates production rules

## CI Failures

- If the CI checks have been executed, look at the results and try to determine if any failures are related to the changes.
