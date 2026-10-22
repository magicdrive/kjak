# Release runbook

A release works as in magicdrive/ark and magicdrive/enma: a tag `vX.Y.Z`
pushed to GitHub runs `.github/workflows/release.yml`, and GoReleaser does the
rest.

## One-time repository setup

1. Settings → General → Releases: enable **immutable releases**. A published
   release can then not be changed (its assets are never replaced), and GitHub
   attests it (`gh release verify`).
2. Secret `PERSONAL_GITHUB_TOKEN` (as in ark and enma): a token with
   contents read/write on `magicdrive/kjak` (the release) and
   `magicdrive/homebrew-tap` (the formula).

## Releasing

```sh
git switch main && git pull --ff-only
make publish tag=v0.1.0      # make test-verbose, then the tag is pushed
```

`release.yml`:

1. **ci** — the whole `ci.yml` on the tagged commit.
2. **release** — the release token is set;
   GoReleaser snapshot and `scripts/verify-dist.sh`; then `goreleaser release
   --clean`: the archives and `kjak_checksums.txt` go to a draft release,
   which GoReleaser publishes, and the formula goes to the root of
   `magicdrive/homebrew-tap` (where the tap keeps all of its formulas); then
   the build provenance of the archives is attested.

A tag with a suffix (`v0.2.0-rc.1`) is published as a pre-release and leaves
the tap alone (`prerelease: auto`, `skip_upload: auto`).

## Verifying a release

```sh
gh release download v0.1.0 --repo magicdrive/kjak
sha256sum -c kjak_checksums.txt
gh release verify v0.1.0 --repo magicdrive/kjak
gh attestation verify kjak_0.1.0_linux_amd64.tar.gz --repo magicdrive/kjak
```

## When a release fails

Fix the cause on main and release the next patch version. Do not edit,
replace or delete a published release.

- Before anything was published (ci, snapshot, verify-dist): nothing to clean
  up; release the fix as the next version.
- GoReleaser stopped with a draft release: delete the draft
  (`gh release delete vX.Y.Z --yes`), then release the next version.
- The release is published, the tap is not updated: commit the `kjak.rb` that
  GoReleaser wrote at the root of the tap by hand, or let the next version
  update it.

A re-run of a release job runs the workflow as it is at the tag, not as it
is on main.

v0.0.1 was released by the earlier workflow (its checksum file is
`checksums.txt`); it is immutable and attested, and is not in the tap. Do not
re-run its run (39766468249).

The tag v0.0.2 (run 39779957468) stopped before anything was published: its
workflow asked for a token that is not set. Nothing of it exists but the tag.
