import assert from "node:assert/strict";
import http, { type IncomingMessage, type ServerResponse } from "node:http";
import test from "node:test";
import { unstable_startWorker } from "wrangler";

const TEST_TIMEOUT_MS = 15_000;

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

type TokenValidationRequest = {
  token: string;
};

const socketBuffers = new WeakMap<WebSocket, SocketBuffer>();
const tokenPrefix = Math.random().toString(36).slice(2, 10);
let tokenSeq = 0;

test("verification returns subscriber response", { timeout: TEST_TIMEOUT_MS }, async () => {
  const relay = await startRelay();
  const token = nextToken();
  const ws = await connect(relay.base, token);
  try {
    const responsePromise = fetch(new URL(`/u/${token}`, relay.base), {
      method: "POST",
      headers: {
        "twitch-eventsub-message-type": "webhook_callback_verification",
      },
      body: JSON.stringify({ challenge: "abc123" }),
    });

    const frame = parseRelayFrame(await waitMessage(ws));
    assert.equal(frame.requires_response, true);
    assert.equal(
      Buffer.from(frame.body, "base64").toString(),
      '{"challenge":"abc123"}',
    );

    ws.send(
      JSON.stringify({
        type: "dispatch_result",
        id: frame.id,
        status: 200,
        headers: { "content-type": "text/plain; charset=utf-8" },
        body: Buffer.from("abc123").toString("base64"),
      }),
    );

    const response = await responsePromise;
    assert.equal(response.status, 200);
    assert.equal(await response.text(), "abc123");
  } finally {
    ws.close();
    await relay.dispose();
  }
});

test("verification fails when no subscriber is connected", { timeout: TEST_TIMEOUT_MS }, async () => {
  const relay = await startRelay();
  try {
    const response = await fetch(new URL(`/u/${nextToken()}`, relay.base), {
      method: "POST",
      headers: {
        "twitch-eventsub-message-type": "webhook_callback_verification",
      },
      body: "{}",
    });
    assert.equal(response.status, 503);
    assert.match(await response.text(), /no relay subscriber connected/);
  } finally {
    await relay.dispose();
  }
});

test("cursor replay resumes after the acknowledged cursor", { timeout: TEST_TIMEOUT_MS }, async () => {
  const relay = await startRelay();
  const token = nextToken();
  const ws = await connect(relay.base, token);
  try {
    await post(relay.base, token, "one");
    await post(relay.base, token, "two");
    const first = parseRelayFrame(await waitMessage(ws));
    const second = parseRelayFrame(await waitMessage(ws));
    assert.equal(Buffer.from(first.body, "base64").toString(), "one");
    assert.equal(Buffer.from(second.body, "base64").toString(), "two");

    const replay = await connect(relay.base, token, `cursor=${first.cursor}`);
    try {
      const replayed = parseRelayFrame(await waitMessage(replay));
      assert.equal(replayed.cursor, second.cursor);
      assert.equal(Buffer.from(replayed.body, "base64").toString(), "two");
      await assertNoMessage(replay);
    } finally {
      replay.close();
    }
  } finally {
    ws.close();
    await relay.dispose();
  }
});

test("expired buffered events are not replayed", { timeout: TEST_TIMEOUT_MS }, async () => {
  const relay = await startRelay({
    BUFFER_TTL_MS: { type: "plain_text", value: "20" },
  });
  const token = nextToken();
  try {
    await post(relay.base, token, "expired");
    await sleep(1_300);

    const ws = await connect(relay.base, token);
    try {
      await assertNoMessage(ws);
    } finally {
      ws.close();
    }
  } finally {
    await relay.dispose();
  }
});

// Self-hosted operators following the README skip TOKEN_VALIDATE_URL and
// RELAY_SHARED_SECRET entirely. wrangler.jsonc must not set either as a
// default `var`, otherwise validateToken would fail closed (503 "relay
// token validation not configured") for every request.
test("token validation is skipped when neither var is configured", {
  timeout: TEST_TIMEOUT_MS,
}, async () => {
  const relay = await startRelay();
  try {
    const response = await post(relay.base, nextToken(), "{}");
    assert.equal(response.status, 202);
  } finally {
    await relay.dispose();
  }
});

test("token validation accepts valid tokens and sends bearer secret", {
  timeout: TEST_TIMEOUT_MS,
}, async () => {
  let gotAuth = "";
  let gotToken = "";
  const validator = await startValidator(async (req, res) => {
    gotAuth = req.headers.authorization ?? "";
    const chunks: Buffer[] = [];
    for await (const chunk of req) chunks.push(Buffer.from(chunk));
    gotToken = parseTokenValidationRequest(Buffer.concat(chunks).toString()).token;
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ valid: true }));
  });
  const relay = await startRelay({
    TOKEN_VALIDATE_URL: { type: "plain_text", value: validator.url },
    RELAY_SHARED_SECRET: { type: "plain_text", value: "shared-secret" },
    ALLOW_INSECURE_TOKEN_VALIDATE_URL: { type: "plain_text", value: "1" },
  });
  const token = nextToken();
  try {
    const response = await post(relay.base, token, "{}");
    assert.equal(response.status, 202);
    assert.equal(gotAuth, "Bearer shared-secret");
    assert.equal(gotToken, token);
  } finally {
    await relay.dispose();
    await validator.close();
  }
});

test("token validation rejects invalid tokens", { timeout: TEST_TIMEOUT_MS }, async () => {
  const validator = await startValidator(async (_req, res) => {
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ valid: false }));
  });
  const relay = await startRelay({
    TOKEN_VALIDATE_URL: { type: "plain_text", value: validator.url },
    RELAY_SHARED_SECRET: { type: "plain_text", value: "shared-secret" },
    ALLOW_INSECURE_TOKEN_VALIDATE_URL: { type: "plain_text", value: "1" },
  });
  try {
    const response = await post(relay.base, nextToken(), "{}");
    assert.equal(response.status, 403);
  } finally {
    await relay.dispose();
    await validator.close();
  }
});

test("token validation misconfiguration fails closed", { timeout: TEST_TIMEOUT_MS }, async () => {
  const relay = await startRelay({
    TOKEN_VALIDATE_URL: {
      type: "plain_text",
      value: "https://example.invalid/validate",
    },
  });
  try {
    const response = await post(relay.base, nextToken(), "{}");
    assert.equal(response.status, 503);
    assert.match(await response.text(), /token validation not configured/);
  } finally {
    await relay.dispose();
  }
});

test("verification response is owned by the selected subscriber", {
  timeout: TEST_TIMEOUT_MS,
}, async () => {
  const relay = await startRelay();
  const token = nextToken();
  const first = await connect(relay.base, token);
  const second = await connect(relay.base, token);
  try {
    const responsePromise = fetch(new URL(`/u/${token}`, relay.base), {
      method: "POST",
      headers: {
        "twitch-eventsub-message-type": "webhook_callback_verification",
      },
      body: "{}",
    });

    const selected = await Promise.race([
      waitMessage(first).then((data) => ({ owner: first, other: second, data })),
      waitMessage(second).then((data) => ({ owner: second, other: first, data })),
    ]);
    const frame = parseRelayFrame(selected.data);

    selected.other.send(
      JSON.stringify({
        type: "dispatch_result",
        id: frame.id,
        status: 418,
        body: Buffer.from("wrong").toString("base64"),
      }),
    );
    await sleep(50);
    selected.owner.send(
      JSON.stringify({
        type: "dispatch_result",
        id: frame.id,
        status: 200,
        body: Buffer.from("right").toString("base64"),
      }),
    );

    const response = await responsePromise;
    assert.equal(response.status, 200);
    assert.equal(await response.text(), "right");
  } finally {
    first.close();
    second.close();
    await relay.dispose();
  }
});

test("verification response sanitizes invalid subscriber status", { timeout: TEST_TIMEOUT_MS }, async () => {
  const relay = await startRelay();
  const token = nextToken();
  const ws = await connect(relay.base, token);
  try {
    const responsePromise = verificationPost(relay.base, token, "{}");
    const frame = parseRelayFrame(await waitMessage(ws));
    ws.send(
      JSON.stringify({
        type: "dispatch_result",
        id: frame.id,
        status: 101,
        body: Buffer.from("bad status").toString("base64"),
      }),
    );

    const response = await responsePromise;
    assert.equal(response.status, 502);
    assert.equal(await response.text(), "bad status");
  } finally {
    ws.close();
    await relay.dispose();
  }
});

test("verification response rejects malformed base64 body safely", { timeout: TEST_TIMEOUT_MS }, async () => {
  const relay = await startRelay();
  const token = nextToken();
  const ws = await connect(relay.base, token);
  try {
    const responsePromise = verificationPost(relay.base, token, "{}");
    const frame = parseRelayFrame(await waitMessage(ws));
    ws.send(
      JSON.stringify({
        type: "dispatch_result",
        id: frame.id,
        status: 200,
        body: "not base64!",
      }),
    );

    const response = await responsePromise;
    assert.equal(response.status, 502);
    assert.match(await response.text(), /invalid relay dispatch response/);
  } finally {
    ws.close();
    await relay.dispose();
  }
});

test("verification response tolerates invalid subscriber headers", { timeout: TEST_TIMEOUT_MS }, async () => {
  const relay = await startRelay();
  const token = nextToken();
  const ws = await connect(relay.base, token);
  try {
    const responsePromise = verificationPost(relay.base, token, "{}");
    const frame = parseRelayFrame(await waitMessage(ws));
    ws.send(
      JSON.stringify({
        type: "dispatch_result",
        id: frame.id,
        status: 200,
        headers: { "bad header": "ignored" },
        body: Buffer.from("ok").toString("base64"),
      }),
    );

    const response = await responsePromise;
    assert.equal(response.status, 200);
    assert.equal(await response.text(), "ok");
  } finally {
    ws.close();
    await relay.dispose();
  }
});

test("async 1xx dispatch result closes subscriber", { timeout: TEST_TIMEOUT_MS }, async () => {
  const relay = await startRelay();
  const token = nextToken();
  const ws = await connect(relay.base, token);
  try {
    await post(relay.base, token, "{}");
    const frame = parseRelayFrame(await waitMessage(ws));
    const closePromise = waitClose(ws);
    ws.send(
      JSON.stringify({
        type: "dispatch_result",
        id: frame.id,
        status: 102,
      }),
    );
    const close = await closePromise;
    assert.equal(close.code, 1011);
  } finally {
    ws.close();
    await relay.dispose();
  }
});

test("token validation times out closed", { timeout: TEST_TIMEOUT_MS }, async () => {
  const validator = await startValidator(async (_req, res) => {
    await sleep(200);
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({ valid: true }));
  });
  const relay = await startRelay({
    TOKEN_VALIDATE_URL: { type: "plain_text", value: validator.url },
    RELAY_SHARED_SECRET: { type: "plain_text", value: "shared-secret" },
    TOKEN_VALIDATE_TIMEOUT_MS: { type: "plain_text", value: "20" },
    ALLOW_INSECURE_TOKEN_VALIDATE_URL: { type: "plain_text", value: "1" },
  });
  try {
    const response = await post(relay.base, nextToken(), "{}");
    assert.equal(response.status, 503);
    assert.match(await response.text(), /relay token validation unavailable/);
  } finally {
    await relay.dispose();
    await validator.close();
  }
});

async function startRelay(
  bindings: RelayBindings = {},
): Promise<RelayHandle> {
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

async function assertNoMessage(ws: WebSocket): Promise<void> {
  await assert.rejects(() => waitMessage(ws, 150), /timed out/);
}

function post(base: string, token: string, body: string): Promise<Response> {
  return fetch(new URL(`/u/${token}`, base), { method: "POST", body });
}

function verificationPost(
  base: string,
  token: string,
  body: string,
): Promise<Response> {
  return fetch(new URL(`/u/${token}`, base), {
    method: "POST",
    headers: {
      "twitch-eventsub-message-type": "webhook_callback_verification",
    },
    body,
  });
}

function waitClose(ws: WebSocket, timeoutMs = 1_000): Promise<CloseEvent> {
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      cleanup();
      reject(new Error("timed out waiting for websocket close"));
    }, timeoutMs);
    const onClose = (event: CloseEvent) => {
      cleanup();
      resolve(event);
    };
    const cleanup = () => {
      clearTimeout(timeout);
      ws.removeEventListener("close", onClose);
    };
    ws.addEventListener("close", onClose);
  });
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

function nextToken(): string {
  tokenSeq += 1;
  return `test${tokenPrefix}${String(tokenSeq).padStart(20, "0")}`;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
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

function parseTokenValidationRequest(data: string): TokenValidationRequest {
  const parsed = JSON.parse(data) as unknown;
  assert.ok(isObject(parsed), "token validation request must be an object");
  const token = parsed.token;
  if (typeof token !== "string") {
    assert.fail("token validation request token must be a string");
  }
  return { token };
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
