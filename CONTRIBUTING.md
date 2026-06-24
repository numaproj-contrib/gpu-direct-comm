# Contributing to gpu-direct-comm

Thank you for your interest in contributing to gpu-direct-comm. This document explains how to get started and what to expect from the contribution process.

## Table of Contents

- [Reporting Issues](#reporting-issues)
- [Development Setup](#development-setup)
- [Code Style](#code-style)
- [Testing Requirements](#testing-requirements)
- [Commit Messages](#commit-messages)
- [Pull Request Guidelines](#pull-request-guidelines)
- [Using Claude Code](#using-claude-code)
- [Code of Conduct](#code-of-conduct)

## Reporting Issues

Before opening a new issue, please search the existing issues to avoid duplicates.

When reporting a bug, include:

- A clear and descriptive title
- Steps to reproduce the problem
- Expected behavior and actual behavior
- Kubernetes version (`kubectl version`)
- Go version (`go version`)
- Relevant logs or error messages

## Development Setup

### Prerequisites

- Go 1.25 or later
- Docker (for building images and running k3d clusters)
- kubectl
- [k3d](https://k3d.io/) (for local development and e2e tests)

### Getting Started

```bash
# Clone the repository
git clone https://github.com/numaproj-contrib/gpu-direct-comm.git
cd gpu-direct-comm

# Install project tool dependencies (controller-gen, envtest, golangci-lint)
make build   # also downloads tools on first run

# Install CRDs into your cluster
make install

# Run unit tests
make test
```

### Useful Make Targets

| Target | Description |
|--------|-------------|
| `make build` | Build the manager binary |
| `make test` | Run unit tests with envtest |
| `make test-e2e` | Run e2e tests with k3d |
| `make lint` | Run golangci-lint |
| `make lint-fix` | Run golangci-lint and apply automatic fixes |
| `make manifests` | Regenerate CRD, RBAC, and webhook YAML |
| `make generate` | Regenerate deepcopy methods |
| `make fmt` | Run `go fmt` on all packages |
| `make vet` | Run `go vet` on all packages |

## Code Style

- Format code with `gofmt` or `goimports` before committing.
- Run `make lint` and fix all warnings before submitting a PR.
- Follow idiomatic Go patterns. See [Effective Go](https://go.dev/doc/effective_go) for reference.
- Keep functions short and focused (under 50 lines when possible).
- Use meaningful names for variables, functions, and types.
- Accept interfaces, return structs.
- Always wrap errors with context using `fmt.Errorf("...: %w", err)`.
- Do not add features or abstractions that are not needed yet (YAGNI).

When modifying or adding a package, update the corresponding `doc.go` file to keep the package-level documentation accurate.

## Testing Requirements

- Write tests for all new functionality before writing the implementation (TDD).
- Use [envtest](https://book.kubebuilder.io/reference/envtest) for controller and webhook tests.
- Aim for 80% or higher test coverage across the packages you change.
- Use the AAA (Arrange-Act-Assert) structure for test readability.
- Use descriptive test function names that explain the behavior under test.

```go
func TestReconcile_CreatesRCT_WhenNumaNetworkIsCreated(t *testing.T) {
    // Arrange
    nn := buildNumaNetwork("default", "my-network", "vf.nvidia.dra.net", "192.168.10.0/24")

    // Act
    result, err := reconciler.Reconcile(ctx, requestFor(nn))

    // Assert
    require.NoError(t, err)
    assert.Equal(t, ctrl.Result{}, result)
}
```

Run the full test suite before submitting:

```bash
make test
make lint
```

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/) format:

```
<type>(<scope>): <description>

<optional body>

Signed-off-by: Your Name <your.email@example.com>
```

### Types

| Type | When to Use |
|------|-------------|
| `feat` | A new feature |
| `fix` | A bug fix |
| `refactor` | A code change that does not add a feature or fix a bug |
| `test` | Adding or updating tests |
| `docs` | Documentation changes |
| `chore` | Build process, tooling, or dependency updates |
| `perf` | Performance improvements |
| `ci` | CI/CD configuration changes |

### DCO Sign-off

All commits must include a DCO (Developer Certificate of Origin) sign-off. Use the `-s` flag when committing:

```bash
git commit -s -m "feat(controller): add health check endpoint"
```

This adds a `Signed-off-by` line to your commit message. It certifies that you wrote the code or have the right to submit it under the project license. See [developercertificate.org](https://developercertificate.org/) for the full text.

Commits without a sign-off will not be merged.

## Pull Request Guidelines

- Submit all pull requests to the **`develop`** branch, not `main`.
- Keep PRs focused on a single change. Do not mix unrelated changes in one PR.
- Write a clear title (under 70 characters).
- Make sure `make test` and `make lint` pass before requesting review.
- Resolve merge conflicts before requesting review.

### PR Description Template

```markdown
## Summary
- What does this PR do?
- Why is this change needed?

## Test Plan
- [ ] Unit tests added or updated
- [ ] `make test` passes
- [ ] `make lint` passes
- [ ] (if applicable) `make test-e2e` passes
```

Write the PR description in plain English. Clear and simple sentences are better than complex phrasing. Non-native English speakers are welcome and valued contributors.

## Using Claude Code

This project includes a `CLAUDE.md` that gives Claude Code full context on the codebase, commands, and architecture.

```bash
claude    # Start Claude Code in the project root
```

Claude Code reads `CLAUDE.md` automatically and can help with implementing features, writing tests, and navigating the codebase.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](https://www.contributor-covenant.org/version/2/1/code_of_conduct/). By participating, you agree to uphold this standard. Please report unacceptable behavior to the project maintainers.
