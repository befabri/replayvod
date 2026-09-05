# Docker upgrade verification

This suite starts a published ReplayVOD image, seeds its database, then replaces
the application with the candidate image while retaining the same Docker volume
and PostgreSQL database. Only the applications run up migrations. The probe reads
and seeds data; it never creates the historical schema. The rollback leg uses it
to execute down SQL while the application is stopped.

## Running locally

Requires Go and a local Linux Docker engine. Baseline images are pinned per
architecture and pulled only when the engine does not hold them, so a run with
cached images needs no registry access. No Twitch credentials or production
data are required. Run from `server/`:

```sh
task test-upgrade
task test-upgrade-full
```

The first command builds the production Dockerfile and checks the latest
baseline with SQLite and PostgreSQL 17. The full command also checks retained
historical baselines and a dataset with 5,000 additional recordings and requests.
It runs on the candidate image's architecture; CI uses native AMD64 and ARM64
runners for the full matrix.

To use an image already built locally:

```sh
UPGRADE_CANDIDATE_IMAGE=replayvod:upgrade-candidate \
UPGRADE_SCOPE=full \
go test -tags upgrade -count=1 -timeout 25m -v ./tests/upgrade
```

The candidate must contain the same SQL as the test build, including uncommitted
changes. Before pulling baselines or starting databases, the harness runs
`/app/replayvod --migration-manifest` in an isolated container and compares SHA-256
checksums for every embedded up and down migration in both backends. Missing,
extra, or changed SQL fails with the affected paths and a rebuild instruction.
Older candidates without this diagnostic must also be rebuilt. Ledger expectations
and rollback SQL then use the test binary's verified embedded files, so later
checkout edits cannot change the SQL under test. This verifies migration content;
it does not attest to every application source file or dependency in the image.

Optional `UPGRADE_BACKEND=sqlite|postgres` and `UPGRADE_BASELINE=v2.7.3` filters
help reproduce one failure. A baseline filter defaults to full scope, including
both dataset sizes, so older baselines work without setting `UPGRADE_SCOPE`. An
explicit `UPGRADE_SCOPE=latest` still restricts selection to the latest baseline.
An invalid scope, an empty selection, unavailable Docker, or a missing image fails the run. Tests are never silently skipped.
`UPGRADE_ARTIFACTS` overrides the default `server/target/upgrade` directory.

Every case uses a unique internal Docker network, database, and data volume.
The network has no external route. HTTP probes run inside the application
container, so they exercise its real server without publishing host ports.
Recording automation and periodic tasks are disabled to keep synthetic data
stable. Startup recovery remains enabled and is checked separately from row
preservation.

## What is checked

- The previous image's source revision and migration ledger match the manifest.
- The previous image serves the seed through the surface every release shares:
  the three sessions authenticate with their roles, the seeded schedule and
  recording are readable, recording bytes stream with Range support, and
  anonymous access is refused. Field level assertions run against the candidate
  only, so an API change never needs a per-release branch in the harness.
- The candidate's embedded migration contents match the test binary before any
  upgrade, including down SQL and edits that leave migration filenames unchanged.
- Existing table rows and values survive, including NULLs, Unicode, access
  rules, encrypted sessions, disabled audio schedules, filters, trigger history,
  legacy requests, and playback progress where the baseline supports it. The
  only accepted differences are the transformations declared in
  `expectations_test.go` for the migrations the candidate adds.
- Previously applied migration versions and timestamps remain unchanged.
- The candidate applies every pending migration, and SQLite integrity and
  foreign-key checks pass. PostgreSQL constraints remain validated and its running
  major version matches `postgres_major` in the manifest.
- Preserved sessions decrypt their historical encrypted tokens through the real
  authentication middleware and retain their original roles. Recording bytes
  and HTTP Range responses remain correct; anonymous recording access fails.
- Administrators can create invitations and approve viewer requests. Viewer
  permissions, duplicate requests, and repeated decisions remain enforced.
- Playback progress can be updated and old tables accept new generated IDs.
- A second candidate startup preserves both historical and newly written data,
  leaves the migration ledger unchanged, and serves the same recordings.
- For the latest released baseline, down migrations and ledger removal execute
  together with the application stopped. The previous image starts and serves
  preserved data, then a second upgrade succeeds. New invitations and schedule
  requests are deliberately lost; approved schedules and playback updates survive.

Separate recovery cases start with RUNNING video and job rows on the previous
schema. A saved remux checkpoint finishes from a pinned synthetic TS segment
without Twitch access, produces playable media, and stays completed after another
restart. Damaged checkpoints fail with an error; a recording with a finalized
part retains its bytes and is classified as partial for cleanup, while an empty
recording is not. Failed recordings retain the existing HTTP 404 behavior.

The normal database suite still tests failed and canceled migrations,
transaction rollback, retry, and concurrent startup. Those precise failure
injection tests complement this full application upgrade path.

Downgrade support is limited to the latest released baseline (currently v2.7.3),
using the documented manual down SQL procedure. Older retained baselines are
upgrade checkpoints, not promises of arbitrary cross-release rollback. Future
migrations must keep this return path working or explicitly revise the rollback
policy and upgrade notes before release.

## Declaring intentional transformations

The preservation check reads every historical table through the candidate
schema using the historical column names and expects identical rows. Added
columns and tables are ignored. A migration that renames a column or table,
drops a table, or legitimately adds rows to an existing table declares that in
`expectations_test.go`, keyed by its version and the historical table name. A rule
applies only to baselines that lack the migration; baselines that already
contain it keep the exact check, and `go test ./tests/upgrade` rejects rules
for unknown migrations or for migrations every retained baseline already has.

An undeclared difference fails with the added migrations and the declaration
site in the message. When the candidate adds no migration, the same failure
means startup modified historical rows, which is never acceptable. Declaring a
rule is a reviewed change to the compatibility contract; do not exclude tables
from the comparison. Column names are captured from the catalog when the
baseline is read, so renames apply to tables that held no rows.

The rollback leg applies the same rules in both directions. A growing table must
keep every row across the downgrade and the second upgrade, and a dropped
table's rows are not expected back, so dropping a table is a rollback data loss
that the upgrade notes must state. A rule for the latest baseline must keep
that leg passing or revise the rollback policy explicitly.

## Historical baselines

`testdata/manifest.json` pins the application images, source commits, PostgreSQL
17 image, and SHA-256 hashes of fixture files and released SQL migrations. The
current baselines are v2.7.3 and v2.7.0, covering installations with and without
saved playback progress. The database schema is produced by those images at
runtime, never by selecting migrations from the candidate checkout. Each baseline
pins its published index digest for provenance and, under `platform_images`, the
linux/amd64 and linux/arm64 manifest digests the harness runs, so a cached image
needs no registry lookup and both architectures coexist in one Docker image
store. Whether a baseline stores playback progress is derived from its released
migrations rather than a hand-maintained flag.
PostgreSQL is created from its pinned index digest on the Docker engine’s platform.
The manifest's `postgres_major` determines the expected image tag and running
server version. The parsed Compose PostgreSQL service must use that tag or the
same pinned digest; a PostgreSQL major upgrade requires updating the manifest and
Compose together and verifying the supported upgrade paths.

The SQL fixtures contain only synthetic values. Session cookies are repeated
`a`, `b`, and `c` characters, with fixture-only tokens encrypted using the
historical session format and `replayvod-upgrade-synthetic-secret-only`. The
small MP4 is generated from a black frame using ffmpeg. It has no external
media dependency. `recording.ts` is a stream-copy remux of that MP4 for
the saved checkpoint in `running.sql`. New recovery scenarios must add frozen
fixtures and checksums rather than generate checkpoints with candidate types.
Do not use these credentials outside the isolated suite.

When adding a released baseline:

From `server/`, the helper below resolves the index to its per-architecture
digests, verifies each image's source revision, reads migration files directly
from the release's Git objects, and appends an entry without changing existing
fixture hashes. It needs Buildx and registry access; the suite itself does not.
It copies the current latest baseline's `seed_files` and `recovery_files`
profiles; `-seed-baseline` selects a different baseline’s profiles:

```sh
go run ./tools/upgrade-baseline \
  -version vMAJOR.MINOR.PATCH \
  -image ghcr.io/befabri/replayvod:MAJOR.MINOR.PATCH@sha256:DIGEST \
  -latest
```

Fetch the published tag first if it is not available locally. Omit `-latest`
when adding an older checkpoint. The helper refuses to overwrite an existing
baseline; after running it:

1. Review the copied seed profile against the released schema. Preserve existing
   fixtures. If different data is needed, add a separate fixture, record its
   checksum, and select it in that baseline's `seed_files` or `recovery_files`
   entries. Keep historical
   entries until deliberately retiring their supported upgrade paths. Never
   regenerate historical fixtures or their checksums during test execution.
2. Run the full suite. Extend assertions alongside new user-visible state and
   migrations. Declare intentional transformations in `expectations_test.go`
   instead of excluding a failing table from preservation checks.

Ordinary `go test ./tests/upgrade` verifies fixture hashes and declared
transformations, and prevents edits to released SQL. The helper's own tests run
with the normal `go test ./...` and also check that the manifest is in the form
the helper writes. Fix released migrations with a new migration. Add historical
fixtures through a reviewed change; do not replace them with current schema dumps.

## CI and release requirements

Public CI checks the latest baseline on AMD64 whenever application, Docker, or
workflow files change. The scheduled and manually dispatched historical job
runs all baselines and both dataset sizes on native AMD64 and ARM64. The release
workflow runs the full matrix plus the existing server, dashboard, and
integration checks for the release commit.

OCI labels are applied before each architecture's candidate is built and tested.
Version tags use normalized semver labels; non-tag builds use a short SHA version.
The same metadata action defines publication tags. Each tested image is exported
as an artifact. Only after all checks succeed does publication validate every
archive's image ID, platform, revision, labels, and layer contents. It pushes
architecture manifests by digest and assembles the release's multi-platform tags,
without publishing intermediate architecture tags or rebuilding the image.

Publication is the `tools/publish-images` command, which uses the container
registry library directly rather than an installed client. Its tests run an
in-memory registry under the normal `go test ./...` and verify both platforms,
unchanged image IDs and labels, repeated publication, and the absence of extra
tags. A corrupt layer must block all registry writes.

The upgrade job uses a Git diff filter, `tools/upgrade-changes`; documentation
and landing changes do not trigger it. Release calls force full coverage
regardless of the diff. Pull requests compare against their merge base,
including deleted paths.

Database snapshots, image provenance, container inspection, application logs,
and test output are retained as CI artifacts, including on failure. Snapshots
are synthetic but include fixture session data; local artifact files use mode 0600
and their directories use mode 0755 (subject to the process umask). Resource
cleanup runs on test failure and in the workflow’s final cleanup step. An
infrastructure failure blocks publication just like a test failure.

Keep PR coverage small and deterministic. Add heavier datasets or historical
checkpoints to the full matrix when they cover a distinct upgrade risk. Record
startup durations as diagnostics; avoid fragile machine-specific timing promises.
Green upgrade checks are part of release readiness alongside the existing tests
and reviewed [upgrade notes](../../../.github/release-notes/next.md). Running this
suite does not tag, push, or publish a release.
