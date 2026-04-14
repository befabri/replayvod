# twitch-gql probe

Headless-Chromium capture of every GQL / HLS request made by twitch.tv
for a given channel. Used to diff against the backend downloader's
`twitch.Client` when the Twitch player flow shifts under us
(PlaybackAccessToken shape, integrity challenges, usher params, etc.).

## Not a test

This is backend diagnostic tooling. It runs under Playwright because
Playwright gives us request/response interception for free, not
because it's a frontend test. The `expect()` at the end is a sanity
check, not an assertion about app behavior.

## Run

```bash
# Default channel: tumblurr
npx playwright test scripts/probes/twitch-gql

# Specific channel
PROBE_CHANNEL=some_broadcaster npx playwright test scripts/probes/twitch-gql
```

Output lands at `scripts/probes/twitch-gql/capture.<channel>.json`.

## What's scrubbed

The writer redacts before writing:

- `user_ip` — your public IP (PII)
- `device_id` — anonymous browser fingerprint (pseudo-identifier)
- `play_session_id` — correlation hint
- `signature` — PlaybackAccessToken HMAC (ephemeral, ~17 min lifetime)
- URL `sig=`, `token=`, `play_session_id=` query params

The GQL query bodies, headers, and response structure are preserved
because that's the whole point of probing.

## Gitignored

Capture files are gitignored (`capture.*.json`). Even scrubbed, don't
commit them — regenerate locally when needed.
