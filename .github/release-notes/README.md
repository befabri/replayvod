# Preparing release notes

GitHub Releases is the public changelog. Keep upcoming changes in
[next.md](next.md) until a release is approved; this directory holds preparation
material, not a second published changelog.

Update the draft alongside user-visible changes. Each entry should explain what
changed, why users care, and whether they need to act. Lead with the symptom or
benefit. Keep API names, database details, and development history out of the
main bullets unless readers need them to use or upgrade the application.

Use New Features, Bug Fixes, Chores, and Upgrade Notes when relevant. Omit empty
sections and combine related changes rather than copying the commit log. Put
compatibility changes, required actions, and rollback limits in Upgrade Notes.

When a release is requested, review commits since the last published tag against
the draft. Confirm the version and release scope, replace the draft comparison
target with the approved tag, and use the reviewed text as the GitHub Release
body. After publication, reset the draft for changes since that release.

Before publication, require the full [upgrade suite](../../server/tests/upgrade/README.md)
and the existing CI checks to pass for the release commit. Review the upgrade
diagnostics and any compatibility notes. After a successful release, add its
published image digest and fixture provenance to the upgrade manifest as the
latest baseline; retain older baselines until their support is deliberately retired.

Image publication and release notes are separate operations: pushing a `v*` tag
triggers the [image workflow](../workflows/release-image.yml), which publishes
AMD64 and ARM64 images to GHCR only after the required checks pass. It publishes
the tested image artifacts without rebuilding them. It does not create a GitHub
Release or its notes.
Editing this draft or committing changes does not authorize tagging or publishing.
