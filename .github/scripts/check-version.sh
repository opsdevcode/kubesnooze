#!/usr/bin/env bash
set -euo pipefail

base_ref="${GITHUB_BASE_REF:-main}"

base_version="$(git describe --tags --abbrev=0 "origin/${base_ref}" 2>/dev/null || true)"
if [[ -z "${base_version}" ]]; then
  echo "No git tags found on ${base_ref}. Create a release tag first."
  exit 1
fi

strip_v() {
  echo "${1#v}"
}

parse_version() {
  local raw
  raw="$(strip_v "$1")"
  if [[ ! "$raw" =~ ^[0-9]+\\.[0-9]+\\.[0-9]+$ ]]; then
    echo "Invalid version: $1"
    return 1
  fi
  IFS='.' read -r major minor patch <<< "$raw"
  echo "$major" "$minor" "$patch"
}

read -r base_major base_minor base_patch < <(parse_version "$base_version")

commit_messages="$(git log "origin/${base_ref}"..HEAD --pretty=format:%s%n%b)"

required="none"
while IFS= read -r line; do
  if [[ "$line" == *"BREAKING CHANGE"* ]] || [[ "$line" =~ ^[A-Za-z]+(\([^)]+\))?!: ]]; then
    required="major"
    break
  fi
  if [[ "$required" != "major" && "$line" =~ ^feat(\([^)]+\))?: ]]; then
    required="minor"
  fi
  if [[ "$required" == "none" && "$line" =~ ^(fix|perf)(\([^)]+\))?: ]]; then
    required="patch"
  fi
done <<< "$commit_messages"

version_greater() {
  local a_major="$1" a_minor="$2" a_patch="$3"
  local b_major="$4" b_minor="$5" b_patch="$6"
  if (( a_major > b_major )); then
    return 0
  fi
  if (( a_major < b_major )); then
    return 1
  fi
  if (( a_minor > b_minor )); then
    return 0
  fi
  if (( a_minor < b_minor )); then
    return 1
  fi
  if (( a_patch > b_patch )); then
    return 0
  fi
  return 1
}

version_bump_valid() {
  local required_bump="$1"
  case "$required_bump" in
    major)
      (( head_major > base_major ))
      ;;
    minor)
      if (( head_major > base_major )); then
        return 0
      fi
      (( head_major == base_major && head_minor > base_minor ))
      ;;
    patch)
      if (( head_major > base_major )); then
        return 0
      fi
      if (( head_major == base_major && head_minor > base_minor )); then
        return 0
      fi
      (( head_major == base_major && head_minor == base_minor && head_patch > base_patch ))
      ;;
    none)
      return 0
      ;;
    *)
      echo "Unknown required bump: $required_bump"
      return 1
      ;;
  esac
}

if [[ "$required" == "none" ]]; then
  echo "No version bump required from commit messages."
  exit 0
fi

echo "Commit messages require a ${required} bump from ${base_version}."
echo "Release tag should be created after merge to satisfy this bump."
