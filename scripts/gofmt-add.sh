#!/usr/bin/env bash
set -euo pipefail

# Script to run gofmt on passed files and stage only files that are not ignored
if [ "$#" -eq 0 ]; then
  exit 0
fi

gofmt -s -w "$@"

for f in "$@"; do
  # Skip files removed or that don't exist
  if [ ! -e "$f" ]; then
    continue
  fi

  # If the file is ignored by git, skip adding it
  if git check-ignore -q -- "$f"; then
    continue
  fi

  git add -- "$f"
done

exit 0
