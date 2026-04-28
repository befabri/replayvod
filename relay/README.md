# replayvod-relay

A dumb webhook relay running on Cloudflare Workers + Durable Objects.
Forwards HTTP `POST` events to subscribed WebSocket clients. No content
inspection, no credential storage, no third-party integrations.

## What this does

1. Accepts HTTPS `POST` at `/u/{token}` with arbitrary body
2. Forwards bytes to whoever has an open WebSocket at `/u/{token}/subscribe`
3. Buffers events for up to 5 minutes so a brief client reconnect doesn't lose them
4. Echoes synchronous webhook-verification responses back to the original POST

## What this does not do

- Verify, parse, or log payload bodies — your client verifies any HMAC
- Store events to disk beyond the 5-minute buffer
- Authenticate to Twitch — it has no Twitch credentials
- Provide end-to-end encryption beyond normal HTTPS/WebSocket transport security
- Tunnel arbitrary HTTP — only the `POST` → WebSocket fan-out above

## Protocol

### Ingest

```
POST /u/{token}
Content-Type: application/json

<body>
```

Response: `202 Accepted` for normal async deliveries. For Twitch EventSub
verification requests, the relay waits for the subscribed client to replay the
request locally and returns that local handler response to Twitch. Token must
match `[a-zA-Z0-9_-]{16,128}`. Bodies larger than 64 KiB are rejected with
`413 Payload Too Large`; EventSub payloads are normally only a few kilobytes.

### Subscribe

```
GET /u/{token}/subscribe?cursor={last_cursor}
Upgrade: websocket
```

On connect, the relay replays any buffered events with a monotonic cursor newer
than `cursor`, then streams new events as they arrive. Each frame:

```json
{
  "id": "uuid",
  "cursor": 42,
  "ts": 1714291200000,
  "headers": { "twitch-eventsub-message-id": "...", "...": "..." },
  "body": "<base64-encoded body>",
  "requires_response": false
}
```

After replaying a frame locally, the subscriber sends a response frame:

```json
{
  "type": "dispatch_result",
  "id": "uuid",
  "status": 200,
  "headers": { "content-type": "text/plain; charset=utf-8" },
  "body": "<base64-encoded response body>"
}
```

For normal async deliveries, a non-2xx/3xx dispatch result causes the relay to
close that subscriber so it reconnects and replays from the last acknowledged
cursor. When `requires_response` is `true`, the relay returns the response to
the original HTTP caller; this is required for Twitch EventSub verification
challenges.

Headers are forwarded verbatim (lower-cased keys) so the receiving client
can reconstruct the original request and verify HMAC signatures itself.
The relay does not parse, validate, or filter headers.

Expired buffered events are pruned by a Durable Object alarm, not only by later
traffic, so the 5-minute buffer does not linger indefinitely during quiet periods.

## Token validation

Self-hosted deployments can leave token validation unset and use any token
matching the pattern above. The hosted Connect deployment sets:

```sh
TOKEN_VALIDATE_URL=https://api.replayvod.com/relay/tokens/validate
RELAY_SHARED_SECRET=...
```

The relay validates every ingest and subscribe request against the cloud Worker;
revoked tokens and inactive Polar subscriptions are rejected before reaching the
Durable Object. Token validation URLs must use HTTPS unless a test deployment
explicitly opts into insecure validation.

The token namespaces a Durable Object; each token gets its own buffer
and connection set. Multiple clients can subscribe to the same token.

## Develop

```sh
npm install
npm run dev
```

## Deploy

```sh
npm run deploy
```

Workers Free tier handles a few hundred light users. Workers Paid ($5/mo)
handles thousands. Durable Object storage is billed by usage; with a 5-minute
TTL the buffer rarely exceeds a few KB per active user.

## License

MIT
