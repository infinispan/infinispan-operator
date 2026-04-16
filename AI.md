# Project Context: Infinispan Operator

The Infinispan Operator is a Kubernetes operator for automating the deployment, scaling, and management of Infinispan clusters on Kubernetes and OpenShift. It is built with Go using the Operator SDK (kubebuilder v3 layout).

## Shared guidelines
- Use the project's established patterns and terminology. When in doubt, follow the conventions of the package you are working in.
- Prefer clarity over cleverness. This is a long-lived open-source project with many contributors.
- Be aware of backward compatibility implications — CRD changes must be compatible with existing Custom Resources in live clusters.

## Coding instructions
When planning and writing code, read and follow the instructions in AI-CODE.md.

## Testing instructions
When writing or modifying tests, read and follow the instructions in AI-TEST.md.

## Issue creation instructions
When creating GitHub issues, read and follow the instructions in AI-ISSUES.md.

## Pull request review instructions
When reviewing PRs, read and follow the instructions in REVIEW.md.
