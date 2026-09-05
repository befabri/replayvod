# Unreleased

Draft for the next GitHub Release. Review the scope and replace `HEAD` with the
approved release tag before publication.

## New Features

- Invite people with a link that expires and can be used once. Administrators
  can grant viewer or administrator access and revoke unused invitations.
- Viewers can request automatic recordings from the Schedules page and follow
  each request's status. Administrators can approve a request with recording
  settings or reject it; viewers can cancel their pending requests.
- Administrators can manage users and the access whitelist. Changes to owner
  accounts and granting the owner role remain restricted to owners.
- Load older request history with **Show more**, keeping existing rows visible
  if the next page needs to be retried.

## Bug Fixes

- Account changes remain consistent when invitations, sign-ins, and role
  changes happen together. Failed invitation redemption does not consume the
  link or leave someone with partially granted access.
- Removing whitelist access also signs the user out. If the operation fails,
  both access and sessions remain intact so it can be retried.
- Failed request decisions report database errors instead of incorrectly
  claiming the request was already decided. Competing approvals cannot create
  duplicate active schedules or start an unapproved recording.
- Schedule pages load their filters and requester names in batches, reducing
  database work as the list grows. Editing a schedule preserves its recording
  history and request attribution.
- Database upgrades preserve existing video-request history and coordinate
  simultaneous server starts. Interrupted migrations can be retried.
- SQLite consistently enables write-ahead logging, waits briefly for busy
  writers, and enforces database relationships after connections are replaced.
- Invitation credentials are hidden in request and panic logs. Webhook
  verification challenges are served as plain text to prevent HTML execution.

## Chores

- Automated upgrade checks now verify that existing data and recording access
  survive updates on SQLite and PostgreSQL 17, including interrupted recording
  recovery and a return to the latest released version. Image publication waits
  for these checks and publishes the exact tested images with their metadata.

## Upgrade Notes

- Database migrations run automatically at startup on PostgreSQL and SQLite.
  Existing recordings, schedules, filters, user access, and playback state are
  retained. Back up the database before updating.
- Recording requests now live on the **Schedules** page. The retired per-video
  request history remains stored, but it is not converted into requests for
  future channel recordings.
- Custom API clients using `videorequest.*` must move to the schedule-request
  procedures. Request-history queries return pages with `items` and an optional
  `next_cursor`; the bundled dashboard handles this automatically.
- Manual rollback to v2.7.3 is tested. Rolling back the invitations migration
  removes invitations and schedule requests created after upgrading. It preserves
  data that predates the upgrade.
- For development installations that already ran the earlier destructive
  migration 045, deleted video-request history requires a backup to recover.
  Updating the migration file cannot restore rows that were already deleted.

[Changes since v2.7.3](https://github.com/befabri/replayvod/compare/v2.7.3...HEAD)
