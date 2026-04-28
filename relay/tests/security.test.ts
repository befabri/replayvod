// Security/correctness regression tests for the relay Worker.

import assert from "node:assert/strict";
import http, { type IncomingMessage, type ServerResponse } from "node:http";
import test from "node:test";
import { unstable_startWorker } from "wrangler";

const TEST_TIMEOUT_MS = 20_000;

type PlainTextBinding = { type: "plain_text"; value: string };
type RelayBindingName =
  | "BUFFER_TTL_MS"
  | "TOKEN_VALIDATE_URL"
  | "RELAY_SHARED_SECRET"
  | "TOKEN_VALIDATE_TIMEOUT_MS"
  | "ALLOW_INSECURE_TOKEN_VALIDATE_URL";
type RelayBindings = Partial<Record<RelayBindingName, PlainTextBinding>>;

type RelayHandle = {
  base: string;
  dispose: () => Promise<void> | void;
};

type ValidatorHandle = {
  url: string;
  close: () => Promise<void>;
};

type MessageWaiter = {
  resolve: (data: string) => void;
  reject: (error: Error) => void;
};

type SocketBuffer = {
  queue: string[];
  waiters: MessageWaiter[];
};

type ValidatorHandler = (
  req: IncomingMessage,
  res: ServerResponse,
) => Promise<void>;

type RelayFrame = {
  id: string;
  cursor: number;
  ts: number;
  headers: Record<string, string>;
  body: string;
  requires_response: boolean;
};

const tokenPrefix = Math.random().toString(36).slice(2, 10);
let tokenSeq = 0;

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

async function startRelay(bindings: RelayBindings = {}): Promise<RelayHandle> {
  const worker = await unstable_startWorker({
    config: "wrangler.jsonc",
    dev: {
      server: { port: 0 },
      inspector: false,
      logLevel: "none",
      watch: false,
    },
    bindings,
  });
  await worker.ready;
  return {
    base: (await worker.url).toString(),
    dispose: () => worker.dispose(),
  };
}

async function startValidator(handler: ValidatorHandler): Promise<ValidatorHandle> {
  const server = http.createServer((req, res) => {
    void handler(req, res).catch((err: Error) => {
      res.writeHead(500, { "content-type": "text/plain" });
      res.end(`${err.message}\n`);
    });
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

function nextToken(): string {
  tokenSeq += 1;
  return `sec${tokenPrefix}${String(tokenSeq).padStart(20, "0")}`;
}

const socketBuffers = new WeakMap<WebSocket, SocketBuffer>();

async function connect(
  base: string,
  token: string,
  query = "",
): Promise<WebSocket> {
  const url = new URL(`/u/${token}/subscribe${query ? `?${query}` : ""}`, base);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  const ws = new WebSocket(url.toString());
  const buffer: SocketBuffer = { queue: [], waiters: [] };
  socketBuffers.set(ws, buffer);
  ws.addEventListener("message", (event) => {
    if (typeof event.data !== "string") {
      const waiter = buffer.waiters.shift();
      waiter?.reject(new Error("unexpected binary websocket message"));
      return;
    }
    const waiter = buffer.waiters.shift();
    if (waiter) {
      waiter.resolve(event.data);
      return;
    }
    buffer.queue.push(event.data);
  });
  await new Promise<void>((resolve, reject) => {
    ws.addEventListener("open", () => resolve(), { once: true });
    ws.addEventListener("error", () => reject(new Error("websocket error")), {
      once: true,
    });
  });
  return ws;
}

function waitMessage(ws: WebSocket, timeoutMs = 1_000): Promise<string> {
  const buffer = socketBuffers.get(ws);
  if (!buffer) return Promise.reject(new Error("websocket is not tracked"));
  const queued = buffer.queue.shift();
  if (queued !== undefined) return Promise.resolve(queued);

  return new Promise((resolve, reject) => {
    const waiter: MessageWaiter = {
      resolve: (data: string) => {
        cleanup();
        resolve(data);
      },
      reject: (error: Error) => {
        cleanup();
        reject(error);
      },
    };
    const timeout = setTimeout(() => {
      cleanup();
      reject(new Error("timed out waiting for websocket message"));
    }, timeoutMs);
    const onError = () => {
      cleanup();
      reject(new Error("websocket error"));
    };
    const cleanup = () => {
      clearTimeout(timeout);
      const index = buffer.waiters.indexOf(waiter);
      if (index !== -1) buffer.waiters.splice(index, 1);
      ws.removeEventListener("error", onError);
    };
    buffer.waiters.push(waiter);
    ws.addEventListener("error", onError);
  });
}

function parseRelayFrame(data: string): RelayFrame {
  const parsed = JSON.parse(data) as unknown;
  assertRelayFrame(parsed);
  return parsed;
}

function assertRelayFrame(value: unknown): asserts value is RelayFrame {
  assert.ok(isObject(value), "relay frame must be an object");
  assert.equal(typeof value.id, "string", "relay frame id must be a string");
  assert.equal(
    typeof value.cursor,
    "number",
    "relay frame cursor must be a number",
  );
  assert.equal(typeof value.ts, "number", "relay frame ts must be a number");
  assert.ok(
    isStringRecord(value.headers),
    "relay frame headers must be string key/value pairs",
  );
  assert.equal(
    typeof value.body,
    "string",
    "relay frame body must be a base64 string",
  );
  assert.equal(
    typeof value.requires_response,
    "boolean",
    "relay frame requires_response must be a boolean",
  );
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isStringRecord(value: unknown): value is Record<string, string> {
  return (
    isObject(value) &&
    Object.values(value).every((entry) => typeof entry === "string")
  );
}
