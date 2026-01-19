# Contributing

Thanks for contributing to KubeSnooze! This guide focuses on how to make
changes safely and how CI enforces conventions in this repo.

## Development workflow

1. Fork or create a branch from `main`.
2. Make your changes locally.
3. Run tests: `go test ./...`
4. Open a pull request.

## Pull requests

All changes to `main` must go through a PR. Direct pushes are blocked for
non‑owners.

Required checks on `main`:

- `CI`
- `Direct Push Guard`
- `Version Check`

Optional: define a repo variable `ALLOWED_DIRECT_PUSH_USERS` with a
comma-separated list of GitHub usernames that may bypass PRs.

## Conventional Commits

This repo uses Conventional Commits. Examples:

- `feat: add cronjob wake override`
- `fix: handle missing selector`
- `perf: reduce API calls`
- `chore: update dependencies`

Add `!` or a `BREAKING CHANGE` footer for major bumps.

## Versioning

Versioning is driven by Conventional Commits and the latest git tag on `main`:

- `feat:` → minor bump
- `fix:` or `perf:` → patch bump
- `BREAKING CHANGE` or `type!:` → major bump

The `Version Check` workflow reports the required semver bump for the PR based
on its commit messages.
