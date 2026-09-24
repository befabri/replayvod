// Security/correctness regression tests for the relay Worker.

import assert from "node:assert/strict";
import http, { type ServerResponse } from "node:http";
import test from "node:test";
import {
  connect,
  parseRelayFrame,
  startRelay,
  startValidator,
  tokenMinter,
  type ValidatorHandle,
  waitMessage,
} from "./support";

const TEST_TIMEOUT_MS = 20_000;
const nextToken = tokenMinter("sec");

test(
  "relay ingest rejects oversized bodies before buffering",
  { timeout: TEST_TIMEOUT_MS },
  async () => {
    // relay/src/relay-do.ts:65 reads the entire request body into memory and
    // :69-80 base64-encodes it and persists into the 5-minute buffer. A
    // valid token can pin large memory + DO storage. EventSub payloads are
    // kilobytes; ingest should reject anything beyond a small max (e.g.
    // 64 KiB) before request.arrayBuffer().
    const relay = await startRelay();
    const token = nextToken();
    const ws = await connect(relay.base, token);
    try {
      // Twitch EventSub payloads are kilobytes; the relay should cap ingest
      // at a small maximum (e.g. 64 KiB) before request.arrayBuffer(). The
      // sizes below all comfortably exceed any legitimate webhook body and
      // are independently confirmed accepted today.
      const oversized: Array<readonly [label: string, size: number]> = [
        ["100 KiB", 100 * 1024],
      ];
      for (const [label, size] of oversized) {
        const res = await fetch(new URL(`/u/${token}`, relay.base), {
          method: "POST",
          body: "A".repeat(size),
        });
        assert.equal(
          res.status,
          413,
          `relay should reject ${label} (${size} bytes)`,
        );
      }

      let stored = 0;
      for (let i = 0; i < oversized.length; i++) {
        try {
          const frame = parseRelayFrame(await waitMessage(ws, 150));
          if (frame && frame.cursor > 0) stored++;
        } catch {
          break;
        }
      }
      assert.equal(
        stored,
        0,
        `oversized bodies must not be broadcast or buffered (${stored}/${oversized.length})`,
      );
    } finally {
      ws.close();
      await relay.dispose();
    }
  },
);

test(
  "relay validator timeout covers response body parsing",
  { timeout: TEST_TIMEOUT_MS },
  async () => {
    // relay/src/index.ts:78-80 clears the AbortController timeout in the
    // `finally` after fetch() resolves, then :89-91 awaits res.json() with no
    // bound. A validator that flushes headers and stalls the body keeps the
    // ingest request hanging well past TOKEN_VALIDATE_TIMEOUT_MS.
    let releaseBody: (() => void) | undefined;
    const validator = await startStallingValidator((res) => {
      releaseBody = () => {
        res.write('{"valid":true}');
        res.end();
      };
    });
    const relay = await startRelay({
      TOKEN_VALIDATE_URL: { type: "plain_text", value: validator.url },
      RELAY_SHARED_SECRET: { type: "plain_text", value: "shared-secret" },
      TOKEN_VALIDATE_TIMEOUT_MS: { type: "plain_text", value: "200" },
      ALLOW_INSECURE_TOKEN_VALIDATE_URL: { type: "plain_text", value: "1" },
    });
    try {
      const start = Date.now();
      const res = await fetch(new URL(`/u/${nextToken()}`, relay.base), {
        method: "POST",
        body: "{}",
      });
      const elapsed = Date.now() - start;
      releaseBody?.();
      assert.ok(
        elapsed < 1_000,
        `ingest should return near the 200ms validator timeout — elapsed ${elapsed}ms`,
      );
      assert.equal(
        res.status,
        503,
        "ingest should fail closed when validator body parsing times out",
      );
    } finally {
      releaseBody?.();
      await relay.dispose();
      await validator.close();
    }
  },
);

test(
  "relay refuses plaintext http validator URLs before sending bearer secret",
  { timeout: TEST_TIMEOUT_MS },
  async () => {
    // relay/src/index.ts:63-71 attaches Authorization: Bearer <secret> to the
    // validator request without checking that TOKEN_VALIDATE_URL is HTTPS. A
    // production typo to http:// silently exfiltrates the shared secret over
    // the wire on every ingest *and* every subscribe. The relay should
    // either refuse to start with a non-HTTPS validator URL or strip the
    // Authorization header before sending.
    const observed = { calls: 0 };
    const validator = await startValidator(async (req, res) => {
      observed.calls++;
      for await (const _ of req) void _;
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify({ valid: true }));
    });
    assert.ok(
      validator.url.startsWith("http://"),
      "validator URL must be plaintext for this test",
    );

    const relay = await startRelay({
      TOKEN_VALIDATE_URL: { type: "plain_text", value: validator.url },
      RELAY_SHARED_SECRET: { type: "plain_text", value: "shared-secret-XYZ" },
    });
    try {
      const token = nextToken();

      const ingest = await fetch(new URL(`/u/${token}`, relay.base), {
        method: "POST",
        body: "{}",
      });
      assert.equal(ingest.status, 503);
      assert.match(await ingest.text(), /validation URL must use https/);

      const wsURL = new URL(`/u/${token}/subscribe`, relay.base);
      wsURL.protocol = wsURL.protocol === "https:" ? "wss:" : "ws:";
      const ws = new WebSocket(wsURL.toString());
      try {
        await assert.rejects(
          () =>
            new Promise<void>((resolve, reject) => {
              ws.addEventListener("open", () => resolve(), { once: true });
              ws.addEventListener(
                "error",
                () => reject(new Error("websocket error")),
                { once: true },
              );
            }),
        );
      } finally {
        ws.close();
      }
      assert.equal(observed.calls, 0, "validator must not be called over http");
    } finally {
      await relay.dispose();
      await validator.close();
    }
  },
);

// -- helpers -----------------------------------------------------------------

// startStallingValidator flushes 200 + JSON content-type, then withholds the
// body until the test calls `release(res)` (captured via the supplied tap).
async function startStallingValidator(
  onRequest: (res: ServerResponse) => void,
): Promise<ValidatorHandle> {
  const server = http.createServer(async (req, res) => {
    for await (const _ of req) void _;
    res.writeHead(200, { "content-type": "application/json" });
    res.flushHeaders?.();
    onRequest(res);
  });
  await new Promise<void>((resolve) => {
    server.listen(0, "127.0.0.1", () => resolve());
  });
  const address = server.address();
  assert.ok(address && typeof address === "object");
  return {
    url: `http://127.0.0.1:${address.port}/validate`,
    close: () =>
      new Promise<void>((resolve, reject) =>
        server.close((err) => (err ? reject(err) : resolve())),
      ),
  };
}

