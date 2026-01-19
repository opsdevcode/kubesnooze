# Contributing

## Pull request requirement

All changes to `main` should go through a pull request, except for the repo
owner (and any optional bypass users you explicitly configure).

To enforce this in GitHub:

1. Create or update a branch protection rule (or ruleset) for `main`:
   - Require a pull request before merging.
   - Require status checks to pass before merging.
   - Require branches to be up to date before merging (recommended).
   - Allow bypass for the repository owner (and only those you trust).
2. Require these checks on `main`:
   - `CI`
   - `Direct Push Guard`
3. Optional: define a repo variable `ALLOWED_DIRECT_PUSH_USERS` with a
   comma-separated list of GitHub usernames that may bypass PRs.

With the rule in place, non-owner direct pushes to `main` will be blocked by
branch protection, and the `Direct Push Guard` workflow will fail if anyone
not in the allowlist tries to push directly.
