#!/usr/bin/env bash
# Prints the CHANGELOG.md section for a version, for use as release notes.
#
#   scripts/release-notes.sh v1.0.0
#
# Fails if the changelog has no section for the version, so a release can
# never go out undocumented.
set -euo pipefail

cd "$(dirname "$0")/.."
version="${1:?usage: release-notes.sh vX.Y.Z}"
version="${version#v}"

notes="$(awk -v v="${version}" '
  /^## \[/ {
    if (found) exit
    if (index($0, "## [" v "]") == 1) { found = 1; next }
  }
  /^\[[^]]+\]: https?:/ { next }
  found { lines[++n] = $0 }
  END {
    first = 1
    while (first <= n && lines[first] ~ /^[[:space:]]*$/) first++
    last = n
    while (last >= first && lines[last] ~ /^[[:space:]]*$/) last--
    for (i = first; i <= last; i++) print lines[i]
  }
' CHANGELOG.md)"

if [[ -z "${notes}" ]]; then
  echo "CHANGELOG.md has no section for ${version}" >&2
  exit 1
fi
printf '%s\n' "${notes}"
