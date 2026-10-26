#!/bin/sh
# Homebrew end-to-end test of the formula Hephaestus generates for kjak.
#
#   1. Hephaestus builds, archives and checksums kjak for E2E_TAG and writes
#      dist/homebrew/kjak.rb exactly as the release would (URLs of the GitHub
#      release, no tap is touched);
#   2. the formula's release URLs are pointed at the local archives (file://)
#      and the version is written in, since Homebrew cannot infer it from a
#      file:// URL; nothing else of the formula changes;
#   3. in a throw-away tap: brew style, brew audit --strict, brew install,
#      the installed kjak (scripts/smoke.sh), brew test, brew uninstall.
#
# It installs into this machine's Homebrew prefix and removes everything it
# created on exit: meant for CI runners and containers.
#
# usage: scripts/homebrew-e2e.sh
#   HEPHAESTUS          the hephaestus command (default: go run ...@v1.2.0)
#   E2E_TAG             the tag to build (default: v0.0.0)
#   KJAK_E2E_SKIP_BUILD=1  use the dist/ of an earlier `hephaestus run
#                       --tag $E2E_TAG --stages build,archive,checksum,formula`
#                       (a container with Homebrew but without Go; set
#                       VERIFY_DIST_SKIP_BUILDINFO=1 there too)
set -eu

repo=$(cd "$(dirname "$0")/.." && pwd)
tag=${E2E_TAG:-v0.0.0}
version=${tag#v}
hephaestus=${HEPHAESTUS:-go run github.com/magicdrive/hephaestus@v1.2.0}
tap="kjak-e2e/t$$"
fail() { echo "homebrew-e2e: $*" >&2; exit 1; }
step() { printf '\n==> %s\n' "$*"; }

export HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_ANALYTICS=1 HOMEBREW_NO_ENV_HINTS=1 \
	HOMEBREW_NO_INSTALL_CLEANUP=1 HOMEBREW_NO_INSTALLED_DEPENDENTS_CHECK=1 \
	HOMEBREW_NO_ASK=1 HOMEBREW_DEVELOPER=1

command -v brew >/dev/null 2>&1 || fail "brew is not on PATH"

# Homebrew 7.0.11 and later load a formula of a tap that is not official only
# once it is trusted (brew trust): brew audit refuses the formula of an
# untrusted tap. Only the throw-away tap is trusted, while it exists (when
# this Homebrew has brew trust); brew untrust also forgets the formula brew
# install trusts.
trusts=false
brew trust --help >/dev/null 2>&1 && trusts=true

cleanup() {
	set +e
	brew uninstall --force "$tap/kjak" >/dev/null 2>&1
	if [ "$trusts" = true ]; then brew untrust --tap "$tap" >/dev/null 2>&1; fi
	brew untap --force "$tap" >/dev/null 2>&1
	[ -z "${work:-}" ] || rm -rf "$work"
}
trap cleanup EXIT

cd "$repo"
if [ "${KJAK_E2E_SKIP_BUILD:-}" != 1 ]; then
	step "Hephaestus: build, archive, checksum, formula for $tag"
	rm -rf "$repo/dist"  # only this run's archives
	# shellcheck disable=SC2086 # HEPHAESTUS is a command line
	$hephaestus run --tag "$tag" --stages build,archive,checksum,formula --fresh
fi
formula="$repo/dist/homebrew/kjak.rb"
[ -f "$formula" ] || fail "no $formula"
sh "$repo/scripts/verify-dist.sh" "$repo/dist" "v$version"

step "Formula with file:// URLs of dist/"
release_url="https://github.com/magicdrive/kjak/releases/download/$tag/"
grep -q "url \"$release_url" "$formula" || fail "the formula does not download from $release_url"
work=$(mktemp -d)
local_formula="$work/kjak.rb"
awk -v from="$release_url" -v to="file://$repo/dist/" -v version="$version" '
	index($0, from) { $0 = substr($0, 1, index($0, from) - 1) to substr($0, index($0, from) + length(from)) }
	/^  license / && !done { print "  version \"" version "\""; done = 1 }
	{ print }
' "$formula" > "$local_formula"
grep -q "^  version \"$version\"$" "$local_formula" || fail "no version line written"
! grep -q "https://github.com/magicdrive/kjak/releases" "$local_formula" || fail "a release URL is left"

step "Throw-away tap $tap"
brew tap-new --no-git "$tap" >/dev/null
if [ "$trusts" = true ]; then brew trust --tap "$tap"; fi
tapdir=$(brew --repository "$tap")
cp "$local_formula" "$tapdir/Formula/kjak.rb"
cat "$tapdir/Formula/kjak.rb"

step "brew style"
brew style --formula "$tap/kjak"

step "brew audit --strict"
brew audit --strict --formula "$tap/kjak"

step "brew install"
brew install --formula "$tap/kjak"

step "Installed kjak"
installed="$(brew --prefix)/bin/kjak"
[ -x "$installed" ] || fail "no executable $installed"
sh "$repo/scripts/smoke.sh" "$installed" "kjak version v$version"

step "brew test"
brew test "$tap/kjak"

step "brew uninstall"
brew uninstall "$tap/kjak"
[ ! -e "$installed" ] || fail "$installed is left after uninstall"

echo
echo "homebrew-e2e: OK ($tag)"
