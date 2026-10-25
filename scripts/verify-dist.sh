#!/bin/sh
# Checks the archives of a Hephaestus run (hephaestus.yaml), without
# publishing anything:
#   - kjak_checksums.txt lists exactly the five archives, each matches it;
#   - each platform has exactly one archive holding kjak (kjak.exe), LICENSE
#     and README.md, and nothing else;
#   - each binary is built for the platform its archive names (when `file`
#     exists) and is statically linked (CGO_ENABLED=0);
#   - the binary for this machine runs and prints the expected version;
#   - `go version -m` of every binary: built by a Go toolchain of the series
#     go.mod's toolchain directive pins (go1.27.x), not older than it, with
#     CGO_ENABLED=0, -trimpath and the archive's GOOS/GOARCH. Needs `go`;
#     VERIFY_DIST_SKIP_BUILDINFO=1 skips this check (a container without Go).
#
# usage: scripts/verify-dist.sh [DIST] [EXPECTED-VERSION-SUBSTRING]   (e.g. v0.1.0, SNAPSHOT)
set -eu

dist=${1:-dist}
gomod="$(cd "$(dirname "$0")/.." && pwd)/go.mod"
want_version=${2:-}
fail() { echo "verify-dist: $*" >&2; exit 1; }

[ -d "$dist" ] || fail "no directory $dist"
cd "$dist"
sums=kjak_checksums.txt
[ -f "$sums" ] || fail "no $sums"

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{ print $1 }'
	else shasum -a 256 "$1" | awk '{ print $1 }'; fi
}

targets="darwin_arm64 darwin_amd64 linux_arm64 linux_amd64 windows_amd64"
[ "$(wc -l < "$sums" | tr -d ' ')" = 5 ] || fail "$sums lists $(wc -l < "$sums" | tr -d ' ') files, want 5"
while read -r sum file; do
	[ -f "$file" ] || fail "$file of $sums is missing"
	[ "$(sha256 "$file")" = "$sum" ] || fail "checksum mismatch: $file"
done < "$sums"
echo "ok   checksums (5 archives)"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# The toolchain go.mod pins (toolchain goX.Y.Z, else go X.Y[.Z]).
want_go=$(awk '$1 == "toolchain" { print substr($2, 3); found = 1; exit } END { if (!found) exit 1 }' "$gomod" 2>/dev/null ||
	awk '$1 == "go" { print $2; exit }' "$gomod")
[ -n "$want_go" ] || fail "no go or toolchain directive in $gomod"
check_buildinfo=yes
if [ "${VERIFY_DIST_SKIP_BUILDINFO:-}" = 1 ]; then
	check_buildinfo=no
	echo "note go version -m is skipped (VERIFY_DIST_SKIP_BUILDINFO=1)"
else
	command -v go >/dev/null 2>&1 || fail "go is needed for go version -m (or VERIFY_DIST_SKIP_BUILDINFO=1)"
fi
# vercmp A B: true when Go version A (1.27.3) is at least B, same X.Y series
go_at_least() {
	awk -v a="$1" -v b="$2" 'BEGIN {
		na = split(a, x, "."); nb = split(b, y, ".")
		if (x[1] != y[1] || x[2] != y[2]) exit 1
		exit !((x[3] + 0) >= (y[3] + 0))
	}'
}
host_os=$(go env GOOS 2>/dev/null || uname -s | tr '[:upper:]' '[:lower:]')
host_arch=$(go env GOARCH 2>/dev/null || uname -m)
case "$host_arch" in x86_64) host_arch=amd64 ;; aarch64) host_arch=arm64 ;; esac
ran=no

for target in $targets; do
	os=${target%_*}
	arch=${target#*_}
	if [ "$os" = windows ]; then ext=zip bin=kjak.exe; else ext=tar.gz bin=kjak; fi
	set -- kjak_*_"$target.$ext"
	[ -f "$1" ] || fail "$target: no .$ext archive"
	[ $# -eq 1 ] || fail "$target: $# archives"
	archive=$1
	awk -v f="$archive" '$2 == f { found = 1 } END { exit !found }' "$sums" || fail "$archive is not in $sums"

	out="$work/$target"
	mkdir -p "$out"
	if [ "$ext" = zip ]; then
		unzip -q "$archive" -d "$out"
	else
		tar -xzf "$archive" -C "$out"
	fi
	contents=$(cd "$out" && find . -type f | sed 's|^\./||' | LC_ALL=C sort | tr '\n' ' ')
	[ "$contents" = "LICENSE README.md $bin " ] || fail "$archive holds: $contents"
	[ "$ext" = zip ] || [ -x "$out/$bin" ] || fail "$archive: $bin is not executable"

	if command -v file >/dev/null 2>&1; then
		desc=$(file -b "$out/$bin")
		case "$os/$arch" in
			linux/amd64) want="ELF 64-bit.*x86-64" ;;
			linux/arm64) want="ELF 64-bit.*(aarch64|ARM aarch64)" ;;
			darwin/amd64) want="Mach-O 64-bit.*x86_64" ;;
			darwin/arm64) want="Mach-O 64-bit.*arm64" ;;
			windows/amd64) want="PE32\+.*x86-64" ;;
		esac
		echo "$desc" | grep -Eq "$want" || fail "$archive: $bin is $desc"
		if [ "$os" = linux ]; then
			echo "$desc" | grep -q "statically linked" || fail "$archive: $bin is not statically linked: $desc"
		fi
	fi

	if [ "$os" = "$host_os" ] && [ "$arch" = "$host_arch" ]; then
		v=$("$out/$bin" version)
		case "$v" in "kjak version v"*) ;; *) fail "$archive: version prints '$v'" ;; esac
		case "$v" in *"$want_version"*) ;; *) fail "$archive: '$v' lacks '$want_version'" ;; esac
		ran=yes
	fi
	if [ "$check_buildinfo" = yes ]; then
		# outside the module, so the go.mod toolchain rule does not apply to
		# the go that reads the binary
		info=$(cd "$work" && go version -m "$out/$bin") || fail "$archive: go version -m failed"
		built_go=$(echo "$info" | awk 'NR == 1 { sub(/^go/, "", $2); print $2 }')
		go_at_least "$built_go" "$want_go" || fail "$archive: built with go$built_go, want go$want_go or a later go${want_go%.*}.x"
		for setting in "CGO_ENABLED=0" "-trimpath=true" "GOOS=$os" "GOARCH=$arch"; do
			echo "$info" | grep -q "^	build	$setting\$" || fail "$archive: build setting $setting missing: $info"
		done
		echo "     $bin built with go$built_go (go.mod toolchain go$want_go), CGO_ENABLED=0, -trimpath"
	fi
	echo "ok   $archive"
done
[ "$ran" = yes ] || echo "note no archive for this machine ($host_os/$host_arch): no binary was run"
echo "verify-dist: OK"
