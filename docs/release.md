# Release runbook

A tag `vX.Y.Z` pushed to GitHub runs `.github/workflows/release.yml`, and
[Hephaestus](https://github.com/magicdrive/hephaestus) (`hephaestus.yaml`)
does the rest: the archives, `kjak_checksums.txt`, the GitHub release and a
pull request with the formula on `magicdrive/homebrew-tap`. Up to v0.0.3 the
releases were made by GoReleaser; Hephaestus keeps their archive names,
platforms, layout and checksum file, and builds byte-identical binaries
(`kjak --version` prints `kjak version vX.Y.Z` as before).

## Repository setup (done once; checked by `make publish-check`)

`make publish` follows the conventions of Hephaestus's
[publish](https://github.com/magicdrive/hephaestus/blob/main/docs/publish.md#conventions):
fixed names and settings that it checks before it tags, and refuses to tag
when one is not met.

1. Settings → General → Releases: **immutable releases** enabled. A
   published release can then not be changed (its assets are never
   replaced), and GitHub attests it (`gh release verify`).
2. Environment **`release`**: deployment branches and tags **selected**,
   **tags only** (`v*`, no branch policy), with the secret
   **`HOMEBREW_TAP_GITHUB_TOKEN`**: a fine-grained token (or a GitHub App
   token) with contents and pull requests read/write on
   `magicdrive/homebrew-tap` only. The publishing job is the only one that
   runs in this environment.
3. An active **branch ruleset** on `main` that requires the status check
   **`ci-gate`** (the job of `ci.yml` that needs every other job).
4. An active **tag ruleset** on `refs/tags/v*` with the rules **deletion**
   and **update** (a release tag is never moved or deleted), and nothing that
   blocks creating the tag.
5. The release itself is published with the workflow's `GITHUB_TOKEN`
   (`contents: write` in the publishing job only). The repository secret
   `PERSONAL_GITHUB_TOKEN`, which GoReleaser used, is no longer read by any
   workflow.

## Releasing

```sh
git switch main && git pull --ff-only
make publish-check v0.1.0    # the preflight only (read-only, plus a fetch of origin/main)
make publish v0.1.0          # preflight, confirm, push the tag, follow the run, verify
```

`make publish vX.Y.Z` (also `VERSION=vX.Y.Z`, or `tag=vX.Y.Z` as before) runs
`hephaestus publish` (`PUBLISH` in the Makefile, Hephaestus pinned by
`HEPHAESTUS_VERSION`). It checks that the version can be released (the
version, a clean `main` equal to `origin/main`, the CI run of the commit on
`main` succeeded, the rulesets, the `release` environment and its secret,
the workflow conventions, and `hephaestus preflight --strict`), asks
(default No; `YES=1` skips the prompt), creates and pushes **one annotated
tag**, then finds and follows the `release.yml` run of that tag and verifies
the release: its assets, checksums and attestations, and the tap pull
request (or, for a prerelease, that the tap was left alone). It never
publishes anything itself; `release.yml` stays the only publisher.

| Command | What it does |
| --- | --- |
| `make publish vX.Y.Z` | the release, as above (the only target that pushes a tag) |
| `make publish-check vX.Y.Z` | the preflight only |
| `make publish-status vX.Y.Z` | what exists for a version: tag, run, release, tap (read-only) |
| `make publish-watch vX.Y.Z` | follow the run of an already pushed tag, then verify |
| `make publish-help` | how publishing works and its safety rules |
| `make preflight tag=vX.Y.Z` | `hephaestus preflight --strict` alone (publish runs it too) |

Exit code 3 of `make publish` means the outcome is unknown (the monitoring
stopped: network, Ctrl-C, timeout), not that the release failed: run
`make publish-status vX.Y.Z` or `make publish-watch vX.Y.Z`.

Until the Hephaestus v1.2.0 adoption, `make publish tag=vX.Y.Z` ran the
tests and a non-strict preflight, then created and pushed a lightweight tag
without asking. It now asks, pushes an annotated tag, requires a green CI
run of the commit on `main` instead of running the tests locally, and follows
the release to the end.

`release.yml`:

1. **ci** — the whole `ci.yml` on the tagged commit.
2. **build** — read-only token, no secrets, no cache: Hephaestus builds,
   archives and checksums the five targets, `scripts/verify-dist.sh` checks
   them (and that the binary prints the tag), and `dist/` (with
   `dist/hephaestus-manifest.json`) is uploaded.
3. **release** — in the `release` environment, nothing is built: the
   checksums of the downloaded `dist/` are checked, the release is planned
   (`Plan the release`, the action's `command: plan`: `prerelease` and
   `tap=publish|skip`), the tap pull request is rehearsed (`Rehearse the
   formula and tap pull request (read-only)`, `--dry-run`: the token, the tap
   layout, a newer formula, the branch), the build provenance of every asset
   (the archives and `kjak_checksums.txt`) is attested, the release is
   published (draft, assets, then published and immutable), and the formula
   pull request is opened on `magicdrive/homebrew-tap` (`Generate the formula
   and open a tap pull request`; branch `kjak-X.Y.Z`, formula `kjak.rb` at
   the tap's root).
4. The owner reviews and merges the tap pull request.

A tag with a suffix (`v0.2.0-rc.1`) is published as a pre-release and leaves
the tap alone (`prerelease: auto`, `skip_upload: auto`): the plan says
`tap=skip`, so the tap token check and both tap steps are skipped and the
tap token is not even passed. The plan step fails a prerelease whose
configuration would update the tap. The two tap step names and the plan step
are conventions that `make publish` verifies; do not rename them.

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
make publish-status vX.Y.Z      # tag, run, release, assets, attestations, tap
make publish-watch vX.Y.Z       # follow the run again, then verify
go run github.com/magicdrive/hephaestus@v1.2.0-rc.1 status --tag vX.Y.Z
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
