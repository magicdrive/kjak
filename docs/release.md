# Release runbook

A tag `vX.Y.Z` pushed to GitHub runs `.github/workflows/release.yml`, and
[Hephaestus](https://github.com/magicdrive/hephaestus) (`hephaestus.yaml`)
does the rest: the archives, `kjak_checksums.txt`, the GitHub release and a
pull request with the formula on `magicdrive/homebrew-tap`. Up to v0.0.3 the
releases were made by GoReleaser; Hephaestus keeps their archive names,
platforms, layout and checksum file, and builds byte-identical binaries
(`kjak --version` prints `kjak version vX.Y.Z` as before).

## Repository setup (done once; checked by `make preflight`)

1. Settings → General → Releases: **immutable releases** enabled. A
   published release can then not be changed (its assets are never
   replaced), and GitHub attests it (`gh release verify`).
2. Environment **`release`**, deployment allowed from tags `v*` only, with
   the secret **`HOMEBREW_TAP_GITHUB_TOKEN`**: a fine-grained token (or a
   GitHub App token) with contents and pull requests read/write on
   `magicdrive/homebrew-tap` only. The publishing job is the only one that
   runs in this environment.
3. The release itself is published with the workflow's `GITHUB_TOKEN`
   (`contents: write` in the publishing job only). The repository secret
   `PERSONAL_GITHUB_TOKEN`, which GoReleaser used, is no longer read by any
   workflow.

## Releasing

```sh
git switch main && git pull --ff-only
make preflight tag=v0.1.0    # read-only checks; needs GITHUB_TOKEN=$(gh auth token)
make publish tag=v0.1.0      # tests, the preflight again (FAIL stops), then the tag is pushed
```

`make preflight` is strict: a WARN (for example no ruleset protecting tags)
fails it too. Read the report; `make publish` stops only on a FAIL.

`release.yml`:

1. **ci** — the whole `ci.yml` on the tagged commit.
2. **build** — read-only token, no secrets, no cache: Hephaestus builds,
   archives and checksums the five targets, `scripts/verify-dist.sh` checks
   them (and that the binary prints the tag), and `dist/` (with
   `dist/hephaestus-manifest.json`) is uploaded.
3. **release** — in the `release` environment, nothing is built: the
   checksums of the downloaded `dist/` are checked, the tap pull request is
   rehearsed (`--dry-run`: the token, the tap layout, a newer formula, the
   branch), the build provenance of the archives is attested, the release is
   published (draft, assets, then published and immutable), and the formula
   pull request is opened on `magicdrive/homebrew-tap` (branch
   `kjak-X.Y.Z`, formula `kjak.rb` at the tap's root).
4. The owner reviews and merges the tap pull request.

A tag with a suffix (`v0.2.0-rc.1`) is published as a pre-release and leaves
the tap alone (`prerelease: auto`, `skip_upload: auto`).

Before tagging, `make release-check tag=vX.Y.Z` builds the archives and the
formula of that version locally (`dist/`, `dist/homebrew/kjak.rb`) and
verifies them; nothing is published.

## Verifying a release

```sh
gh release download v0.1.0 --repo magicdrive/kjak
sha256sum -c kjak_checksums.txt
gh release verify v0.1.0 --repo magicdrive/kjak
gh attestation verify kjak_0.1.0_linux_amd64.tar.gz --repo magicdrive/kjak
```

## When a release fails

Look first, without changing anything:

```sh
go run github.com/magicdrive/hephaestus@v1.1.0 status --tag vX.Y.Z
```

Then:

- **ci or build failed**: nothing was published. Fix the cause on main and
  release the next patch version (the tag is never moved).
- **release failed before "Publish the GitHub release"** (the tap token,
  the rehearsal, the attestation): nothing is public. Fix the cause (for
  example the environment secret) and re-run the failed jobs of the run.
- **"Publish the GitHub release" failed**: re-run the failed jobs. A draft
  left by the run is found and reused; identical assets are skipped.
  Hephaestus never deletes a release or replaces a published asset.
- **The release is published, the tap pull request failed**: re-run the
  failed job (the release step is then a no-op on the immutable release),
  or run `hephaestus run --stages formula,tap` with the `dist/` of the run.
- **A published release is wrong**: do not edit, replace or delete it;
  release the next version.

A re-run of a release job runs the workflow as it is at the tag, not as it
is on main. To roll back the move to Hephaestus, revert its pull request
(it restores `.goreleaser.yml` and the GoReleaser workflow).

## History

v0.0.1 was released by the earlier workflow (its checksum file is
`checksums.txt`); it is immutable and attested, and is not in the tap. Do not
re-run its run (39766468249).

The tag v0.0.2 (run 39779957468) stopped before anything was published: its
workflow asked for a token that is not set. Nothing of it exists but the tag.
v0.0.2 was then released by run 39783292007, and v0.0.3 (GoReleaser, the last
one) by run 39959748642.
