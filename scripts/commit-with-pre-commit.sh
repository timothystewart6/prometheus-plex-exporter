#!/usr/bin/env bash
set -euo pipefail

# Helper to run pre-commit, allow hooks to fix files, re-run, and then commit.
# Usage: scripts/commit-with-pre-commit.sh -m "commit message"

usage() {
  echo "Usage: $0 -m \"commit message\""
  exit 2
}

msg=""
while getopts ":m:" opt; do
  case $opt in
    m) msg="$OPTARG" ;;
    *) usage ;;
  esac
done
shift $((OPTIND-1))

if [ -z "$msg" ]; then
  usage
fi

# Run pre-commit; allow it to modify files
pre-commit run --all-files || true

# If pre-commit modified files, they will be staged by their hooks (if configured),
# or remain unstaged. Stage all changes except ignored files.
# We use git add --all but skip ignored files via check-ignore filter.

# Add non-ignored changes
while IFS= read -r -d '' f; do
  if git check-ignore -q -- "$f"; then
    continue
  fi
  git add -- "$f"
done < <(git ls-files --modified --others --exclude-standard -z)

# Run pre-commit again to ensure hooks pass now that files are staged
pre-commit run --all-files

# Perform commit
git commit -m "$msg"

exit 0
