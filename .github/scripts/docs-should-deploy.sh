#!/usr/bin/env bash
set -euo pipefail

event_name="${GITHUB_EVENT_NAME:-}"
base_ref="${GITHUB_BASE_REF:-main}"

if [[ "$event_name" == "pull_request" ]]; then
  range="origin/${base_ref}..HEAD"
else
  before="${GITHUB_EVENT_BEFORE:-}"
  sha="${GITHUB_SHA:-}"
  if [[ -n "$before" && -n "$sha" ]]; then
    range="${before}..${sha}"
  else
    range="origin/${base_ref}..HEAD"
  fi
fi

commit_messages="$(git log "${range}" --pretty=format:%s)"
docs_re='^docs(\([^)]+\))?:'
docs_commit=false
while IFS= read -r line; do
  if [[ $line =~ $docs_re ]]; then
    docs_commit=true
    break
  fi
done <<< "$commit_messages"

if ! $docs_commit; then
  echo "No docs conventional commit found."
  echo "deploy=false" >> "$GITHUB_OUTPUT"
  exit 0
fi

if git diff --name-only "${range}" | grep -qE '^(docs/|mkdocs\.yml)'; then
  echo "deploy=true" >> "$GITHUB_OUTPUT"
else
  echo "Docs commit found but no docs changes."
  echo "deploy=false" >> "$GITHUB_OUTPUT"
fi
