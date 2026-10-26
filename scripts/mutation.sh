#!/bin/sh
# Mutation testing of the security-critical paths of kjak: each mutant of
# scripts/mutants.tsv (a negated condition, a dropped check, a replaced
# return) is applied to a copy of the module, the packages that guard that
# code are tested, and the copy is restored and compared (cmp) with the
# original file before the next mutant. The working tree is never changed.
#
# A mutant is KILLED when a test fails, SURVIVED when every test passes
# (a missing test), INVALID when the mutated code does not build (a broken
# mutant: fix its entry). Exit status: 0 when every mutant was killed, 1
# when one survived, 2 for an invalid or stale mutant (its code is not
# found exactly once: update scripts/mutants.tsv with the code).
#
# usage: scripts/mutation.sh [--check] [ID-REGEX]
#   --check   only check that every mutant applies (fast; no test is run)
#   ID-REGEX  only the mutants whose id matches (grep -E)
#
# scripts/mutants.tsv, one mutant per line, tab-separated:
#   ID  FILE  PACKAGES  OLD  NEW  WHAT
# OLD must occur exactly once in FILE; OLD and NEW may use \n and \t
# (printf %b). PACKAGES are space-separated ./paths. # starts a comment.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
spec=$root/scripts/mutants.tsv
check=false
if [ "${1:-}" = "--check" ]; then
	check=true
	shift
fi
only=${1:-}
tab=$(printf '\t')
timeout=${MUTATION_TEST_TIMEOUT:-600s}

work=$(mktemp -d "${TMPDIR:-/tmp}/kjak-mutation.XXXXXX")
cleanup() { rm -rf "$work"; }
trap cleanup EXIT
trap 'cleanup; exit 130' INT TERM

# the copy of the module: tracked and untracked (not ignored) files
(
	cd "$root"
	git ls-files --cached --others --exclude-standard | while IFS= read -r f; do
		[ -f "$f" ] && printf '%s\n' "$f"
	done | tar -cf - -T -
) | (cd "$work" && tar -xf -)
mkdir -p "$work/.mutation"

# replace FILE: the one occurrence of $OLD by $NEW (environment, so that
# awk does not interpret their backslashes); prints the count found
replace() {
	awk '
		{ buf = buf $0 "\n" }
		END {
			old = ENVIRON["OLD"]; new = ENVIRON["NEW"]
			n = 0; rest = buf; out = ""
			while ((i = index(rest, old)) > 0) {
				n++
				out = out substr(rest, 1, i - 1) new
				rest = substr(rest, i + length(old))
			}
			out = out rest
			if (n == 1) printf "%s", out > ENVIRON["OUT"]
			print n
		}
	' "$1"
}

total=0 killed=0 survived=0 invalid=0
survivors=""
while IFS="$tab" read -r id file pkgs old new what; do
	case "$id" in '' | '#'*) continue ;; esac
	if [ -n "$only" ] && ! printf '%s\n' "$id" | grep -Eq -- "$only"; then
		continue
	fi
	total=$((total + 1))
	target=$work/$file
	backup=$work/.mutation/original
	mutated=$work/.mutation/mutated
	if [ ! -f "$target" ]; then
		echo "STALE    $id: no file $file"
		invalid=$((invalid + 1))
		continue
	fi
	OLD=$(printf '%b' "$old") NEW=$(printf '%b' "$new") OUT=$mutated
	export OLD NEW OUT
	rm -f "$mutated"
	n=$(replace "$target")
	if [ "$n" != 1 ]; then
		echo "STALE    $id: the code of the mutant is found $n times in $file"
		invalid=$((invalid + 1))
		continue
	fi
	if cmp -s "$target" "$mutated"; then
		echo "STALE    $id: the mutant changes nothing"
		invalid=$((invalid + 1))
		continue
	fi
	if $check; then
		echo "OK       $id"
		continue
	fi

	cp "$target" "$backup"
	cp "$mutated" "$target"
	# the packages and their tests must build (-run '^$': no test runs);
	# PACKAGES is a list (word splitting wanted)
	# shellcheck disable=SC2086
	if ! (cd "$work" && go test -count=1 -run '^$' $pkgs) >"$work/.mutation/log" 2>&1; then
		echo "INVALID  $id: does not build"
		sed 's/^/    /' "$work/.mutation/log" | head -n 5
		invalid=$((invalid + 1))
	else
		# shellcheck disable=SC2086
		if (cd "$work" && go test -count=1 -timeout "$timeout" $pkgs) >"$work/.mutation/log" 2>&1; then
			echo "SURVIVED $id: $what ($file)"
			survived=$((survived + 1))
			survivors="$survivors $id"
		else
			echo "KILLED   $id"
			killed=$((killed + 1))
		fi
	fi
	cp "$backup" "$target"
	if ! cmp -s "$backup" "$target" || ! cmp -s "$root/$file" "$target"; then
		echo "mutation: $file was not restored" >&2
		exit 2
	fi
done <"$spec"

if $check; then
	echo "mutation --check: $total mutant(s), $invalid stale"
	[ "$invalid" -eq 0 ] || exit 2
	exit 0
fi
echo "mutation: $total mutant(s): $killed killed, $survived survived, $invalid invalid"
[ "$total" -gt 0 ] || { echo "mutation: no mutant selected" >&2; exit 2; }
if [ "$total" -gt "$invalid" ]; then
	echo "mutation score: $((killed * 100 / (total - invalid)))%"
fi
[ -z "$survivors" ] || echo "survived:$survivors"
[ "$invalid" -eq 0 ] || exit 2
[ "$survived" -eq 0 ] || exit 1
