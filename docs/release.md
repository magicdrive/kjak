# Release runbook

Pushing an **annotated tag** `vX.Y.Z` on main approves a release. From there
`.github/workflows/release.yml` publishes it without further input.

Principles:

- **Build once** — the archives are built one time; every later step uses
  those exact files (the workflow artifact). Nothing attested is rebuilt.
- **Publish once** — a tag whose release exists is refused. Assets are never
  replaced.
- **Recover forward** — a broken release is fixed by a new version.
- **Fail closed** — any failed check stops the release where it is.

## One-time repository setup

1. Settings → General → Releases: enable **immutable releases** (the publish
   step refuses a release that is not immutable).
2. A ruleset for `main` requiring the status check **CI Gate**.
3. A ruleset for tags `v*`: restrict creation to maintainers; block updates
   and deletions.
4. Secret `TAP_GITHUB_TOKEN`: a fine-grained token with contents read/write
   on `magicdrive/homebrew-tap` only.
5. Actions → General: workflow permissions read-only by default (the release
   jobs ask for what they need).

## Releasing

```sh
git switch main && git pull --ff-only
make publish tag=v0.1.0
```

`make publish` checks the tag first (`make publish-check tag=v0.1.0` does only
this, pushing nothing): `vX.Y.Z[-pre]`, on `main` with a clean working tree,
`HEAD` equal to `origin/main`, no such tag locally or on origin. Then it runs
`make check`, creates the annotated tag and pushes only that tag. By hand,
the same is:

```sh
make check                                    # locally, optional: CI runs it again
git tag -a v0.1.0 -m "kjak v0.1.0"
git push origin v0.1.0
```

Then watch the run of "Release":

| step | job | what it does | fails when |
|---|---|---|---|
| 1 | Tag Validation | semver `vX.Y.Z[-pre]`, annotated, not moved, on main, no release yet | any of these |
| 2 | CI | the whole `ci.yml` on the tagged commit, CI Gate included | any CI job |
| 3 | Build, Verify and Attest | `goreleaser release --clean --skip=publish` (v2.18.2), `scripts/verify-dist.sh dist vX.Y.Z`, formula from `checksums.txt`, build provenance attestation of the archives and `checksums.txt`, upload as the `release` artifact | config check, build, archive contents, checksums, version, attestation |
| 4 | Draft Release | refuses any release **or draft** of the tag (drafts of a failed run are not bound to the tag and invisible to the tag job), then `gh release create --draft --verify-tag` with exactly the artifact's files | an existing release/draft, upload |
| 5 | Pre-publish Verification | downloads the draft's assets: same names, byte-identical to the build, checksums, `gh attestation verify` (signer workflow `release.yml`, source ref the tag, GitHub-hosted runner) | any difference |
| 6 | Publish (Immutable Release) | `gh release edit --draft=false`; the release must report `immutable: true`; `gh release verify` and `gh release verify-asset` for every asset (release attestation) | not immutable, attestation |
| 7 | Homebrew Formula | the formula's 4 URLs downloaded and hashed against `checksums.txt`; `Formula/kjak.rb` committed to `magicdrive/homebrew-tap` | mismatch, token missing, push |
| 8 | Post-release Verification | public download, checksums, attestations (Linux, macOS); `brew install magicdrive/tap/kjak`, `kjak version`, `brew test` (macOS) | any |

A pre-release tag (`v0.2.0-rc.1`) is published as a pre-release, not latest,
and **never updates the Homebrew formula**: the formula is not written
(`scripts/formula.sh` refuses a pre-release), the Homebrew job is skipped, and
post-release verification checks the assets and attestations only.

### Assets

```
kjak_0.1.0_darwin_amd64.tar.gz   kjak_0.1.0_linux_amd64.tar.gz
kjak_0.1.0_darwin_arm64.tar.gz   kjak_0.1.0_linux_arm64.tar.gz
kjak_0.1.0_windows_amd64.zip     checksums.txt (SHA-256)
```

Each archive holds `kjak` (`kjak.exe`), `LICENSE` and `README.md`. Binaries
are `CGO_ENABLED=0`, `-trimpath`, version `vX.Y.Z` in `kjak version`.

### Why GoReleaser does not publish

GoReleaser v2.18.2 builds, archives and checksums. Its `brews` publisher is
deprecated (`goreleaser check` exits non-zero with it), and it pushes the
formula in the same step that uploads the release — before the release could
be verified, and while a draft's URLs are not public. kjak therefore writes
the formula itself (`scripts/formula.sh`, from the published `checksums.txt`)
and pushes it only after the release is published and immutable. GoReleaser's
own release is disabled in `.goreleaser.yaml`, and the build runs with
`--skip=publish`.

Locally, `make snapshot` runs the same build as a snapshot (nothing is
published, no token is needed) and writes `dist/kjak.rb`.

## Verifying a release by hand

```sh
gh release download v0.1.0 --repo magicdrive/kjak --dir /tmp/kjak
cd /tmp/kjak && sha256sum -c checksums.txt
gh attestation verify kjak_0.1.0_linux_amd64.tar.gz --repo magicdrive/kjak
gh release verify v0.1.0 --repo magicdrive/kjak
gh release verify-asset v0.1.0 kjak_0.1.0_linux_amd64.tar.gz --repo magicdrive/kjak
```

## Recovery

| failed at | state | do |
|---|---|---|
| 1–3 | nothing published | fix on main; delete the tag (`git push --delete origin vX.Y.Z`, if the ruleset allows a maintainer to) or, preferably, tag the next patch version |
| 4–5 | a draft exists (not public) | delete the draft (`gh release delete vX.Y.Z --yes`); re-run the workflow from the start, or tag the next version. Do not upload by hand |
| 6, not immutable | the release is public and mutable | enable immutable releases; do not edit this release; release the next patch version |
| 6, attestation | published | investigate; do not change assets; release the next version |
| 7 | published, tap not updated | fix the cause (token, permissions) and **re-run the failed job** (it uses the same artifact); or commit `kjak.rb` of the artifact to the tap by hand |
| 8 | published, tap updated | investigate; a bad binary is fixed by a new version, never by replacing assets |

Never rebuild and upload binaries for an existing tag: the attestations and
checksums name the original build.
