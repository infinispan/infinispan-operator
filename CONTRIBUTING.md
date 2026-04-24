Guidelines for contributing to the Infinispan Operator
====

Contributions from the community are essential in keeping the Infinispan Operator strong and successful.

This guide focuses on how to contribute back to the Infinispan Operator using GitHub pull requests.

## Legal

All original contributions to the Infinispan Operator are licensed under the
[ASL - Apache License](https://www.apache.org/licenses/LICENSE-2.0),
version 2.0 or later, or, if another license is specified as governing the file or directory being
modified, such other license.

All contributions are subject to the [Developer Certificate of Origin (DCO)](https://developercertificate.org/).

## Getting Started

If you are just getting started with Git, GitHub and/or contributing to the Infinispan Operator there are a
few prerequisite steps:

* Make sure you have a [GitHub account](https://github.com/signup/free)
* [Fork](https://help.github.com/articles/fork-a-repo/) the
  Infinispan Operator [repository](https://github.com/infinispan/infinispan-operator).
* Install [Go](https://go.dev/doc/install) (see `go.mod` for the required version)
* Install [Make](https://www.gnu.org/software/make/)
* Have access to a Kubernetes or OpenShift cluster for E2E testing

## Building

```shell
make manager
```

## Running Tests

```shell
# Unit tests
make test

# Lint
make lint

# E2E tests (requires a running cluster)
make infinispan-test
make cache-test
```

## Create a topic branch

Create a "topic" branch on which you will work. The convention is to name the branch
using the issue number. If there is not already an issue covering the work you
want to do, create one. Assuming you will be working from the main branch and working
on issue 1234:

```shell
git checkout -b 1234/my-feature origin/main
```

## Code

Code away...

After modifying CRD types, always run:

```shell
make manifests    # Regenerate CRD YAML, RBAC, webhooks
make generate     # Regenerate deepcopy methods
```

## Commit

* Make commits of logical units.
* Be sure to start the commit messages with the issue number you are working on, in the format `[#1234] Summary`. This allows easy cross-referencing on GitHub.
* Avoid formatting changes to existing code as much as possible: they make the intent of your patch less clear.
* Make sure you have added the necessary tests for your changes.
* Run the tests to assure nothing else was accidentally broken:

```shell
make test
make lint
```

_Prior to committing, if you want to pull in the latest upstream changes (highly
appreciated by the way), please use rebasing rather than merging. Merging creates
"merge commits" that really muck up the project timeline._

```shell
git pull --rebase upstream main
```

## Use of Generative AI

Generative AI tools may be used to assist in writing code, tests, or documentation, provided that you fully understand every change you submit. The goal is to keep the Operator's code consistent and high-quality while respecting reviewers' limited time.

If you use generative AI to assist with your contribution, all the following are required:

* You understand the change. You must be able to explain what your code does and why. Submitting AI-generated code you do not understand is not acceptable.
* You engage with review feedback. You are expected to respond to questions and comments from reviewers. If you use AI to help draft responses, you must edit and proofread them to ensure they are accurate and address the reviewer's point.
* You can revise the code yourself. If a reviewer requests changes, you are responsible for addressing them, even if your AI tool is unable to produce a suitable fix.
* You disclose AI agents usage. Include a note in the PR description indicating the usage of AI agents for generating complete solutions from a prompt (i.e. you do not need to mention a simple AI autocomplete). This helps reviewers calibrate their review.
* You ensure licensing compliance. All generated code must be released under the Apache License, Version 2.0, the same license as the Infinispan Operator. You are responsible for verifying that the tools you use do not introduce additional licensing restrictions.

## Submit

* Push your changes to a topic branch in your fork of the repository.
* Initiate a [pull request](http://help.github.com/send-pull-requests/).
* Cross-reference the issue number in the pull request description.
