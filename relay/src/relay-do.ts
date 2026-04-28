import { DurableObject } from "cloudflare:workers";

const BUFFER_TTL_MS = 5 * 60 * 1000;
const RESPONSE_TIMEOUT_MS = 8 * 1000;
const MAX_BODY_BYTES = 64 * 1024;
const EVENTSUB_MESSAGE_TYPE = "twitch-eventsub-message-type";
const EVENTSUB_VERIFICATION = "webhook_callback_verification";

type EventRow = {
  seq: number;
  id: string;
  ts: number;
  headers: string;
  body: string;
};

type Frame = {
  id: string;
  cursor: number;
  ts: number;
  headers: Record<string, string>;
  body: string;
  requires_response: boolean;
};

type DispatchResult = {
  type: "dispatch_result";
  id: string;
  status: number;
  headers?: Record<string, string>;
  body?: string;
};

type PendingDispatch = {
  ws: WebSocket;
  timer: ReturnType<typeof setTimeout>;
  resolve: (result: DispatchResult) => void;
};

export class RelayDO extends DurableObject<CloudflareBindings> {
  private pending = new Map<string, PendingDispatch>();
  private bufferTtlMs = BUFFER_TTL_MS;

  constructor(ctx: DurableObjectState, env: CloudflareBindings) {
    super(ctx, env);
    const configuredTtl = Number(
      (env as CloudflareBindings & { BUFFER_TTL_MS?: string }).BUFFER_TTL_MS,
    );
    if (Number.isFinite(configuredTtl) && configuredTtl > 0) {
      this.bufferTtlMs = configuredTtl;
    }
    ctx.blockConcurrencyWhile(async () => {
      this.migrateEventsTable();
      await this.scheduleNextPrune();
    });
  }

  async fetch(request: Request): Promise<Response> {
    if (request.method === "POST") return this.ingest(request);
    if (request.headers.get("Upgrade")?.toLowerCase() === "websocket")
      return this.subscribe(request);
    return new Response("not found\n", { status: 404 });
  }

  private async ingest(request: Request): Promise<Response> {
    const bodyResult = await readBodyWithLimit(request, MAX_BODY_BYTES);
    if (!bodyResult.ok) {
      return new Response("relay payload too large\n", { status: 413 });
    }
    const buf = bodyResult.body;
    const id = crypto.randomUUID();
    const ts = Date.now();
    const headers = serializeHeaders(request.headers);
    const body = base64Encode(buf);
    const requiresResponse =
      headers[EVENTSUB_MESSAGE_TYPE] === EVENTSUB_VERIFICATION;

    this.pruneExpired(ts);

    // Verification challenges are synchronous request/response handshakes:
    // Twitch must receive the local handler's challenge echo, so they are not
    // useful as async replay-buffer entries.
    const cursor = requiresResponse
      ? 0
      : this.storeEvent(id, ts, headers, body);
    if (!requiresResponse) await this.scheduleNextPrune(ts);

    const frame: Frame = {
      id,
      cursor,
      ts,
      headers,
      body,
      requires_response: requiresResponse,
    };
    const payload = JSON.stringify(frame);
    const sockets = this.ctx.getWebSockets();

    if (sockets.length === 0) {
      if (requiresResponse) {
        return new Response("no relay subscriber connected\n", { status: 503 });
      }
      return new Response("accepted, no subscriber connected\n", {
        status: 202,
      });
    }

    if (requiresResponse) {
      const target = sockets[0];
      const response = this.waitForDispatch(id, target);
      try {
        target.send(payload);
      } catch {
        this.clearPending(id);
        return new Response("relay subscriber unavailable\n", { status: 503 });
      }
      return responseFromDispatch(await response);
    }

    this.broadcast(payload, sockets);
    return new Response("ok\n", { status: 202 });
  }

  private subscribe(request: Request): Response {
    const pair = new WebSocketPair();
    const client = pair[0];
    const server = pair[1];

    this.ctx.acceptWebSocket(server);

    const cursor = Number(new URL(request.url).searchParams.get("cursor") ?? 0);
    this.replay(server, Number.isFinite(cursor) ? cursor : 0);

    return new Response(null, { status: 101, webSocket: client });
  }

  private replay(ws: WebSocket, cursor: number) {
    const cutoff = Date.now() - this.bufferTtlMs;
    const rows = this.ctx.storage.sql
      .exec<EventRow>(
        "SELECT seq, id, ts, headers, body FROM events WHERE seq > ? AND ts >= ? ORDER BY seq ASC",
        cursor,
        cutoff,
      )
      .toArray();
    for (const r of rows) {
      const frame: Frame = {
        id: r.id,
        cursor: r.seq,
        ts: r.ts,
        headers: JSON.parse(r.headers),
        body: r.body,
        requires_response: false,
      };
      try {
        ws.send(JSON.stringify(frame));
      } catch {
        return;
      }
    }
  }

  webSocketMessage(ws: WebSocket, message: string | ArrayBuffer) {
    const text =
      typeof message === "string" ? message : new TextDecoder().decode(message);
    let result: DispatchResult;
    try {
      result = JSON.parse(text) as DispatchResult;
    } catch {
      return;
    }
    if (result.type !== "dispatch_result" || !result.id) return;

    const pending = this.pending.get(result.id);
    if (pending) {
      if (pending.ws !== ws) return;
      clearTimeout(pending.timer);
      this.pending.delete(result.id);
      pending.resolve(result);
      return;
    }

    if (isFailedDispatch(result)) {
      try {
        ws.close(1011, "dispatch failed");
      } catch {}
    }
  }

  webSocketClose(ws: WebSocket, code: number) {
    for (const [id, pending] of this.pending) {
      if (pending.ws !== ws) continue;
      clearTimeout(pending.timer);
      this.pending.delete(id);
      pending.resolve(dispatchFailure(id, "relay subscriber disconnected\n"));
    }
    try {
      ws.close(code, "closed");
    } catch {}
  }

  webSocketError() {}

  async alarm() {
    this.pruneExpired();
    await this.scheduleNextPrune();
  }

  private migrateEventsTable() {
    const columns = this.ctx.storage.sql
      .exec<{ name: string }>("PRAGMA table_info(events)")
      .toArray();

    if (columns.length > 0 && !columns.some((c) => c.name === "seq")) {
      this.ctx.storage.sql.exec("DROP TABLE IF EXISTS events_v1_backup");
      this.ctx.storage.sql.exec(
        "ALTER TABLE events RENAME TO events_v1_backup",
      );
    }

    this.ctx.storage.sql.exec(`
      CREATE TABLE IF NOT EXISTS events (
        seq INTEGER PRIMARY KEY AUTOINCREMENT,
        id TEXT NOT NULL UNIQUE,
        ts INTEGER NOT NULL,
        headers TEXT NOT NULL,
        body TEXT NOT NULL
      )
    `);
    this.ctx.storage.sql.exec(
      "CREATE INDEX IF NOT EXISTS idx_events_ts ON events (ts)",
    );

    if (columns.length > 0 && !columns.some((c) => c.name === "seq")) {
      this.ctx.storage.sql.exec(`
        INSERT INTO events (id, ts, headers, body)
        SELECT id, ts, headers, body FROM events_v1_backup ORDER BY ts, id
      `);
      this.ctx.storage.sql.exec("DROP TABLE events_v1_backup");
    }
  }

  private storeEvent(
    id: string,
    ts: number,
    headers: Record<string, string>,
    body: string,
  ): number {
    this.ctx.storage.sql.exec(
      "INSERT INTO events (id, ts, headers, body) VALUES (?, ?, ?, ?)",
      id,
      ts,
      JSON.stringify(headers),
      body,
    );
    const row = this.ctx.storage.sql
      .exec<{ seq: number }>("SELECT last_insert_rowid() AS seq")
      .toArray()[0];
    return row.seq;
  }

  private pruneExpired(now = Date.now()) {
    this.ctx.storage.sql.exec(
      "DELETE FROM events WHERE ts < ?",
      now - this.bufferTtlMs,
    );
  }

  private async scheduleNextPrune(now = Date.now()) {
    const row = this.ctx.storage.sql
      .exec<{ ts: number | null }>("SELECT MIN(ts) AS ts FROM events")
      .toArray()[0];
    if (row?.ts == null) return;
    await this.ctx.storage.setAlarm(
      Math.max(row.ts + this.bufferTtlMs + 1000, now + 1000),
    );
  }

  private broadcast(payload: string, sockets = this.ctx.getWebSockets()) {
    for (const ws of sockets) {
      try {
        ws.send(payload);
      } catch {
        // socket dead; runtime will surface webSocketClose
      }
    }
  }

  private clearPending(id: string) {
    const pending = this.pending.get(id);
    if (!pending) return;
    clearTimeout(pending.timer);
    this.pending.delete(id);
  }

  private waitForDispatch(id: string, ws: WebSocket): Promise<DispatchResult> {
    return new Promise((resolve) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        resolve(dispatchFailure(id, "relay dispatch timeout\n"));
      }, RESPONSE_TIMEOUT_MS);
      this.pending.set(id, { ws, timer, resolve });
    });
  }
}

function dispatchFailure(id: string, message: string): DispatchResult {
  return {
    type: "dispatch_result",
    id,
    status: 503,
    headers: { "content-type": "text/plain; charset=utf-8" },
    body: base64EncodeText(message),
  };
}

function isFailedDispatch(result: DispatchResult): boolean {
  return (
    !Number.isInteger(result.status) ||
    result.status < 200 ||
    result.status > 399
  );
}

function responseFromDispatch(result: DispatchResult): Response {
  const status = safeResponseStatus(result.status);
  const body = safeBase64Decode(result.body ?? "");
  if (!body.ok) {
    return new Response("invalid relay dispatch response\n", {
      status: 502,
      headers: { "content-type": "text/plain; charset=utf-8" },
    });
  }
  const headers = new Headers();
  for (const [name, value] of Object.entries(result.headers ?? {})) {
    if (isSkippedResponseHeader(name)) continue;
    try {
      headers.set(name, String(value));
    } catch {
      // Subscriber-controlled headers should never crash the relay.
    }
  }
  return new Response(statusAllowsBody(status) ? body.value : null, {
    status,
    headers,
  });
}

function safeResponseStatus(status: number): number {
  if (Number.isInteger(status) && status >= 200 && status <= 599) {
    return status;
  }
  return 502;
}

function statusAllowsBody(status: number): boolean {
  return status !== 204 && status !== 205 && status !== 304;
}

function serializeHeaders(headers: Headers): Record<string, string> {
  const out: Record<string, string> = {};
  headers.forEach((value, key) => {
    out[key.toLowerCase()] = value;
  });
  return out;
}

function isSkippedResponseHeader(name: string): boolean {
  switch (name.toLowerCase()) {
    case "connection":
    case "content-length":
    case "transfer-encoding":
    case "upgrade":
    case "keep-alive":
      return true;
    default:
      return false;
  }
}

function base64Encode(buffer: ArrayBufferLike): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary);
}

async function readBodyWithLimit(
  request: Request,
  limit: number,
): Promise<{ ok: true; body: ArrayBuffer } | { ok: false }> {
  const contentLength = request.headers.get("content-length");
  if (contentLength && Number(contentLength) > limit) {
    try {
      await request.body?.cancel();
    } catch {}
    return { ok: false };
  }
  if (!request.body) return { ok: true, body: new ArrayBuffer(0) };

  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > limit) {
      try {
        await reader.cancel();
      } catch {}
      return { ok: false };
    }
    chunks.push(value);
  }

  const out = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return { ok: true, body: out.buffer };
}

function base64EncodeText(text: string): string {
  return base64Encode(new TextEncoder().encode(text).buffer);
}

function base64Decode(value: string): Uint8Array {
  if (!value) return new Uint8Array();
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

function safeBase64Decode(
  value: string,
): { ok: true; value: Uint8Array } | { ok: false } {
  try {
    return { ok: true, value: base64Decode(value) };
  } catch {
    return { ok: false };
  }
}
