#!/usr/bin/env bash
# Prints the section of a CPAN-format Changes file for one released version,
# for use as release notes. Called by .github/workflows/release.yml.
#
#   ./scripts/changes-section.sh VERSION [FILE]   (default: Changes in the repo root)
#
# The section is every line after the line whose first field is VERSION, up
# to the next line that starts with a non-space character. The two-space
# bullet indent is stripped (continuation lines keep the rest of theirs), as
# are surrounding blank lines and CR line endings.
#
# Fails when VERSION is empty or {{$NEXT}}, or when the section is missing or
# empty, so a tag without a stamped Changes section cannot publish a release.
set -euo pipefail

VERSION="${1:-}"
FILE="${2:-$(dirname "$0")/../Changes}"

if [ -z "$VERSION" ]; then
  echo "usage: $0 VERSION [FILE]" >&2
  exit 1
fi
if [ "$VERSION" = '{{$NEXT}}' ]; then
  echo "ERROR: {{\$NEXT}} is the unreleased section, not a version" >&2
  exit 1
fi
if [ ! -r "$FILE" ]; then
  echo "ERROR: cannot read $FILE" >&2
  exit 1
fi

# Exit codes 3 and 4 stay clear of awk's own 2 for fatal errors.
status=0
CHANGES_VERSION="$VERSION" awk '
  { sub(/\r$/, "") }
  found && /^[^ \t]/ { exit }
  found {
    sub(/^  /, "")
    lines[++n] = $0
    if ($0 !~ /^[ \t]*$/) { if (!first) first = n; last = n }
    next
  }
  # Concatenating "" forces a string comparison: 0.10 must not match 0.1.
  /^[^ \t]/ && $1 "" == ENVIRON["CHANGES_VERSION"] "" { found = 1 }
  END {
    if (!found) exit 3
    if (!last) exit 4
    for (i = first; i <= last; i++) print lines[i]
  }
' "$FILE" || status=$?

case "$status" in
  0) ;;
  3) echo "ERROR: $FILE has no section for $VERSION (still under {{\$NEXT}}?)" >&2; exit 1 ;;
  4) echo "ERROR: the $VERSION section in $FILE is empty" >&2; exit 1 ;;
  *) exit "$status" ;;
esac
