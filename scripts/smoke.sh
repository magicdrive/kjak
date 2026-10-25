#!/bin/sh
# Smoke test of a built kjak binary, run as a user runs it, in a throw-away
# HOME / XDG_CONFIG_HOME / working directory, without network access:
#   - --version, -v and `version` print exactly the expected line;
#   - --help, -h and every command's --help succeed;
#   - an unknown command and no command exit 2 (the exit status contract);
#   - config init / path / validate, feather add / list, group set, template
#     list and `send --dry-run` work on a fresh configuration.
#
# usage: scripts/smoke.sh BINARY [EXPECTED-VERSION-LINE]
#   e.g. scripts/smoke.sh build/smoke/kjak "kjak version v0.0.0-smoke"
set -eu

[ $# -ge 1 ] || { echo "usage: $0 BINARY [EXPECTED-VERSION-LINE]" >&2; exit 2; }
bin=$1
want_version=${2:-}
fail() { echo "smoke: $*" >&2; exit 1; }

case "$bin" in /* | [A-Za-z]:[\\/]*) ;; *) bin="$(pwd)/$bin" ;; esac
[ -f "$bin" ] || fail "no binary $bin"

work=$(mktemp -d 2>/dev/null || mktemp -d -t kjak-smoke)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/home" "$work/xdg" "$work/project"

# Only the variables the run needs: nothing of the caller's configuration,
# credentials or keychain settings reaches kjak.
kjak() {
	(cd "$work/project" && env -i \
		PATH="$PATH" HOME="$work/home" USERPROFILE="$work/home" \
		XDG_CONFIG_HOME="$work/xdg" TMPDIR="${TMPDIR:-/tmp}" \
		SYSTEMROOT="${SYSTEMROOT:-}" TEMP="${TEMP:-}" TMP="${TMP:-}" \
		KJAK_SMOKE_WEBHOOK="https://hooks.slack.com/services/T000/B000/XXXX" \
		"$bin" "$@")
}

# must DESCRIPTION ARGS...: exit 0, stdout kept in $out
must() {
	what=$1; shift
	set +e
	out=$(kjak "$@" 2>"$work/stderr")
	rc=$?
	set -e
	[ "$rc" = 0 ] || { cat "$work/stderr" >&2; fail "$what: kjak $* exited $rc"; }
}

# exits WANT ARGS...: the exit status is WANT
exits() {
	want=$1; shift
	set +e
	kjak "$@" >/dev/null 2>&1
	got=$?
	set -e
	[ "$got" = "$want" ] || fail "kjak $*: exit $got, want $want"
}

must "version" version
v=$out
case "$v" in "kjak version "?*) ;; *) fail "version prints '$v'" ;; esac
[ -z "$want_version" ] || [ "$v" = "$want_version" ] || fail "version prints '$v', want '$want_version'"
must "--version" --version
[ "$out" = "$v" ] || fail "--version prints '$out', version '$v'"
must "-v" -v
[ "$out" = "$v" ] || fail "-v prints '$out', version '$v'"
echo "ok   $v"

for flag in --help -h; do
	must "$flag" "$flag"
	case "$out" in *"Usage:"*"Commands:"*) ;; *) fail "$flag lacks the usage" ;; esac
done
for cmd in send feather group config template version; do
	must "$cmd --help" "$cmd" --help
	[ -n "$out" ] || fail "$cmd --help printed nothing"
done
echo "ok   help"

exits 2
exits 2 no-such-command
echo "ok   exit status of usage errors"

must "config init" config init
[ -f "$work/xdg/kjak/config.toml" ] || fail "config init wrote no $work/xdg/kjak/config.toml"
must "config path" config path -o json
case "$out" in *xdg*kjak*config.toml*) ;; *) fail "config path: $out" ;; esac
must "config validate" config validate
must "feather add" feather add smoke_slack --type slack --secret-env KJAK_SMOKE_WEBHOOK
must "feather list" feather list
case "$out" in *smoke_slack*) ;; *) fail "feather list: $out" ;; esac
must "group set" group set smoke smoke_slack
must "template list" template list
must "send --dry-run" send --to smoke --dry-run "smoke test"
echo "ok   configuration and a dry run (nothing sent)"

echo "smoke: OK"
