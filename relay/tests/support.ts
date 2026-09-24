import assert from "node:assert/strict";
import http, { type IncomingMessage, type ServerResponse } from "node:http";
import { unstable_startWorker } from "wrangler";

export type PlainTextBinding = { type: "plain_text"; value: string };

export type RelayBindingName =
  | "BUFFER_TTL_MS"
  | "TOKEN_VALIDATE_URL"
  | "RELAY_SHARED_SECRET"
  | "TOKEN_VALIDATE_TIMEOUT_MS"
  | "ALLOW_INSECURE_TOKEN_VALIDATE_URL";

export type RelayBindings = Partial<Record<RelayBindingName, PlainTextBinding>>;

export type RelayHandle = {
  base: string;
  dispose: () => Promise<void> | void;
};

export type ValidatorHandle = {
  url: string;
  close: () => Promise<void>;
};

export type ValidatorHandler = (
  req: IncomingMessage,
  res: ServerResponse,
) => Promise<void>;

export type MessageWaiter = {
  resolve: (data: string) => void;
  reject: (error: Error) => void;
};

export type SocketBuffer = {
  queue: string[];
  waiters: MessageWaiter[];
};

export type RelayFrame = {
  id: string;
  cursor: number;
  ts: number;
  headers: Record<string, string>;
  body: string;
  requires_response: boolean;
};

export const socketBuffers = new WeakMap<WebSocket, SocketBuffer>();

export async function startRelay(
  bindings: RelayBindings = {},
): Promise<RelayHandle> {
  const worker = await unstable_startWorker({
    config: "wrangler.jsonc",
    envFiles: ["tests/empty.env"],
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

export async function connect(
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

export function waitMessage(ws: WebSocket, timeoutMs = 1_000): Promise<string> {
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

export async function startValidator(
  handler: ValidatorHandler,
): Promise<ValidatorHandle> {
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

export function parseRelayFrame(data: string): RelayFrame {
  const parsed = JSON.parse(data) as unknown;
  assertRelayFrame(parsed);
  return parsed;
}

export function assertRelayFrame(value: unknown): asserts value is RelayFrame {
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

export function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

export function isStringRecord(
  value: unknown,
): value is Record<string, string> {
  return (
    isObject(value) &&
    Object.values(value).every((entry) => typeof entry === "string")
  );
}

export function tokenMinter(label: string): () => string {
  const prefix = Math.random().toString(36).slice(2, 10);
  let seq = 0;
  return () => {
    seq += 1;
    return `${label}${prefix}${String(seq).padStart(20, "0")}`;
  };
}
