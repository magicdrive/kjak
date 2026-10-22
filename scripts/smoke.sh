#!/bin/sh
# Smoke test of a kjak binary on this OS: commands that need no network, in
# a temporary configuration directory. Nothing is sent anywhere.
#
# usage: scripts/smoke.sh PATH-TO-KJAK
set -eu

fail() { echo "smoke: $*" >&2; exit 1; }
kjak=${1:?usage: smoke.sh PATH-TO-KJAK}
[ -f "$kjak" ] || [ -f "$kjak.exe" ] || fail "no binary $kjak"
case "$kjak" in /*|[A-Za-z]:*) ;; *) kjak="$(pwd)/$kjak" ;; esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
# Git Bash on Windows: give kjak.exe a Windows path (C:/...), which it reads
# as absolute
if command -v cygpath >/dev/null 2>&1; then tmp=$(cygpath -m "$tmp"); fi
mkdir -p "$tmp/work"
cd "$tmp/work"
export XDG_CONFIG_HOME="$tmp/xdg"
export KJAK_SMOKE_HOOK="https://hooks.example.invalid/services/T/B/smoke"

expect() { # expect STATUS COMMAND...
	want=$1; shift
	set +e; "$@" > "$tmp/out" 2>&1; got=$?; set -e
	[ "$got" = "$want" ] || { cat "$tmp/out" >&2; fail "'$*' exited $got, want $want"; }
}

expect 0 "$kjak" version
grep -q '^kjak version ' "$tmp/out" || fail "version output: $(cat "$tmp/out")"
expect 2 "$kjak"
expect 0 "$kjak" --help
expect 0 "$kjak" config init
expect 0 "$kjak" feather add smoke --type slack --secret-env KJAK_SMOKE_HOOK
expect 0 "$kjak" group set team smoke
expect 0 "$kjak" config validate
expect 0 "$kjak" send --to team --dry-run --level warning "smoke test"
grep -q 'plan  smoke (slack)' "$tmp/out" || fail "dry run output: $(cat "$tmp/out")"
expect 0 "$kjak" send --to team --dry-run -o json "smoke test"
grep -q '"schema_version": 1' "$tmp/out" || fail "json output: $(cat "$tmp/out")"
expect 2 "$kjak" send --to team --stdin "two inputs"
expect 2 "$kjak" feather add native --type os
printf '[templates.t]\nbody = "x"\n' > .kjak.toml
expect 2 "$kjak" send --to team --dry-run "untrusted project"
expect 0 "$kjak" config trust --yes
expect 0 "$kjak" send --to team --dry-run --template t
echo "smoke: OK ($("$kjak" version))"
