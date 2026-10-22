#!/bin/sh
# Checks and runs the critical tests of test/critical-tests.txt.
#
#   scripts/critical-tests.sh check              the manifest only
#   scripts/critical-tests.sh run COUNT [FLAGS]  check, then run every critical
#                                                test COUNT times (FLAGS go to
#                                                go test, e.g. -race)
#
# The check fails when the manifest is empty, has a malformed or duplicate
# line, names a test that does not exist in its package, or misses a
# TestCritical_ test of the code. The run fails unless every listed test
# passed exactly COUNT times: a -run pattern that matches nothing would pass.
set -eu

# CRITICAL_ROOT: the module to check (default: this repository; the tests of
# this script use a fixture module)
cd "${CRITICAL_ROOT:-$(dirname "$0")/..}"
manifest=test/critical-tests.txt
module=$(go list -m)
fail() { echo "critical-tests: $*" >&2; exit 1; }

[ -f "$manifest" ] || fail "no $manifest"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# the manifest without comments and blank lines
grep -v '^[[:space:]]*#' "$manifest" | grep -v '^[[:space:]]*$' > "$work/listed" || true
[ -s "$work/listed" ] || fail "$manifest lists no test"

awk 'NF != 2 { printf "line %d: expected \"<package> <test>\": %s\n", NR, $0; bad = 1 }
     $2 !~ /^TestCritical_[A-Za-z0-9_]+$/ { printf "line %d: %s does not follow TestCritical_<Name>\n", NR, $2; bad = 1 }
     END { exit bad }' "$work/listed" >&2 || fail "malformed manifest"

dups=$(awk '{ print $2 }' "$work/listed" | sort | uniq -d)
[ -z "$dups" ] || fail "listed twice: $dups"

# the critical tests that exist, as "<package> <test>"
go test -list '^TestCritical_' ./... > "$work/list.out" 2>&1 || { cat "$work/list.out" >&2; fail "go test -list failed"; }
awk -v mod="$module" '
	/^TestCritical_/ { names[++n] = $1; next }
	/^(ok|\?)[[:space:]]/ {
		pkg = $2; sub("^" mod "/?", "", pkg); if (pkg == "") pkg = "."
		for (i = 1; i <= n; i++) print pkg, names[i]
		n = 0
	}' "$work/list.out" | sort > "$work/existing"

sort "$work/listed" > "$work/listed.sorted"
missing=$(comm -23 "$work/listed.sorted" "$work/existing")
[ -z "$missing" ] || fail "listed but not found in its package:
$missing"
unlisted=$(comm -13 "$work/listed.sorted" "$work/existing")
[ -z "$unlisted" ] || fail "TestCritical_ tests missing from $manifest:
$unlisted"

total=$(wc -l < "$work/listed" | tr -d ' ')
echo "critical-tests: manifest OK ($total tests)"

[ "${1:-check}" = run ] || exit 0
shift
count=${1:?usage: critical-tests.sh run COUNT [go test flags]}
shift

pkgs=$(awk '{ print "./" $1 }' "$work/listed" | sort -u)
names=$(awk '{ print $2 }' "$work/listed" | paste -sd '|' -)
echo "critical-tests: go test -count=$count $* -run '^($names)\$' ($total tests)"
# shellcheck disable=SC2086 # $pkgs and "$@" split on purpose
if ! go test -count="$count" -v "$@" -run "^($names)\$" $pkgs > "$work/run.out" 2>&1; then
	grep -E '^(--- FAIL|FAIL|panic:|[[:space:]]+[a-z_]+_test\.go:[0-9]+:)' "$work/run.out" >&2 || tail -50 "$work/run.out" >&2
	fail "a critical test failed"
fi

# every listed test passed exactly COUNT times (top-level "--- PASS:" lines)
awk -v want="$count" '
	NR == FNR { listed[$2] = 1; next }
	/^--- PASS: TestCritical_/ { passed[$3]++ }
	END {
		for (t in listed) if (passed[t] != want) { printf "%s passed %d times, want %d\n", t, passed[t], want; bad = 1 }
		exit bad
	}' "$work/listed" "$work/run.out" >&2 || fail "not every critical test ran $count times"
echo "critical-tests: $total tests passed $count times each"
