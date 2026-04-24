# Issue Creation Instructions

## Issue Types

The Infinispan Operator uses plain GitHub issues (no issue templates are currently defined). Use labels for classification:

| Label              | When to Use                                                          |
|--------------------|----------------------------------------------------------------------|
| **kind/bug**       | Something is broken or behaving incorrectly                          |
| **kind/enhancement** | New functionality or enhancement to existing functionality         |
| **area/housekeeping** | Cleanup, refactoring, dependency updates — not a bug or feature   |

## Required Information by Type

### Bug Report
- **Title** — concise description of the bug
- **Description** — what is broken
- **Expected behavior** — what should happen
- **Actual behavior** — what happens instead
- **How to reproduce** — steps, CR YAML, or cluster configuration to reproduce
- **Environment** — Kubernetes/OpenShift version, operator version, Infinispan version

Note: security vulnerabilities should be reported privately to security@infinispan.org, not as public issues.

### Feature / Enhancement
- **Title** — concise description of the feature
- **Description** — what the feature does and why it is needed
- **Implementation ideas** (optional) — technical approach, affected CRDs, API changes

### Housekeeping
- **Title** — concise description of the task
- **Description** — what needs to be done
- **Implementation ideas** (optional) — technical approach

## Conventions

- Issue titles should be concise and descriptive
- Reference related issues and PRs using `#number` format
- When creating issues from code investigation, include file paths, CRD kinds, and controller names where relevant
- Commit messages reference issues using `[#00000] Summary` format

## External Resources

- Community discussions: [GitHub Discussions](https://github.com/infinispan/infinispan/discussions)
- Live chat: [Infinispan Zulip](https://infinispan.zulipchat.com/)
