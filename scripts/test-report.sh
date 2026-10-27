#!/bin/sh
# test-report.sh FILE: the readable result of `go test -json ./... > FILE`.
#
# Prints the output of every failed test and package (what plain `go test`
# prints for them), then every skipped test with its reason, then the
# counts. With GITHUB_STEP_SUMMARY set, the skipped tests are also written
# there as a table. CI keeps FILE as an artifact, so the skips of each
# platform can be compared run by run (docs/development.md).
#
# Exit status: 0 when the report was printed, whatever the tests did (the
# caller keeps `go test`'s own status); 1 when FILE cannot be read.
set -eu

if [ $# -ne 1 ] || [ ! -r "$1" ]; then
	echo "usage: $0 FILE (the output of go test -json)" >&2
	exit 1
fi
file=$1

# The key of an event: its package and its test ("" for the package).
# Output lines that only frame a test (=== RUN, --- SKIP, ...) are left out
# of a skip's reason.
jq -rs '
  def key: .Package + " " + (.Test // "");
  def reason: [ .[] | select(.Output | test("^\\s*(=== |--- )") | not) | .Output | sub("^\\s+"; "") | sub("\\s+$"; "") ] | map(select(. != "")) | join(" / ");
  . as $all
  | ([ $all[] | select(.Action == "fail") | key ] | unique) as $failed
  | ([ $all[] | select(.Action == "skip" and .Test != null) | key ] | unique) as $skipped
  | ([ $all[] | select(.Action == "output") ] | group_by(key) | map({ key: (.[0] | key), value: . }) | from_entries) as $out
  | ([ $all[] | select(.Action == "pass" and .Test != null) ] | length) as $passed
  | ( if ($failed | length) > 0 then "## failed", ($failed[] as $k | ($out[$k] // [])[] | .Output | sub("\n$"; "")) else empty end ),
    "## skipped (\($skipped | length))",
    ( $skipped[] as $k | "SKIP \($k): \(($out[$k] // []) | reason)" ),
    "## \($passed) passed, \([ $failed[] | select(endswith(" ") | not) ] | length) failed, \($skipped | length) skipped (tests and subtests)"
' "$file"

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
	{
		echo "### Skipped tests (${RUNNER_OS:-$(uname -s)})"
		echo
		echo "| package | test | reason |"
		echo "|---|---|---|"
		jq -rs '
		  def key: .Package + " " + (.Test // "");
		  . as $all
		  | ([ $all[] | select(.Action == "output") ] | group_by(key) | map({ key: (.[0] | key), value: . }) | from_entries) as $out
		  | [ $all[] | select(.Action == "skip" and .Test != null) ] | unique_by(key)[]
		  | . as $e
		  | ([ ($out[$e | key] // [])[] | .Output | select(test("^\\s*(=== |--- )") | not) | sub("^\\s+"; "") | sub("\\s+$"; "") | select(. != "") ] | join(" / ") | gsub("\\|"; "\\|")) as $why
		  | "| \($e.Package) | \($e.Test) | \($why) |"
		' "$file"
	} >>"$GITHUB_STEP_SUMMARY"
fi
