// End-to-end relay verification against the deployed prod relay (or any
// running relay), without depending on real Twitch traffic. Uses a
// synthetic Twitch EventSub `stream.online` payload and the verification
// challenge handshake described in relay/README.md.
//
// Inputs (env):
//   RELAY_BASE_URL  default https://relay.replayvod.com
//   RELAY_TOKEN     required; a relay token issued by the cloud worker
//                   (POST /me/tokens on a paid account)
//
// Run:  RELAY_TOKEN=... npx tsx relay/scripts/verify-prod.ts

const baseURL = (process.env.RELAY_BASE_URL ?? "https://relay.replayvod.com")
  .replace(/\/+$/, "");
const token = process.env.RELAY_TOKEN;
if (!token) {
  console.error("RELAY_TOKEN is required");
  process.exit(2);
}

const ingestURL = `${baseURL}/u/${token}`;

type Frame = {
  id: string;
  cursor: number;
  ts: number;
  headers: Record<string, string>;
  body: string;
  requires_response: boolean;
};

const decoder = new TextDecoder();
function decodeBase64(s: string): string {
  return decoder.decode(Uint8Array.from(atob(s), (c) => c.charCodeAt(0)));
}
function encodeBase64(s: string): string {
  return btoa(unescape(encodeURIComponent(s)));
}

function streamOnlinePayload(): string {
  return JSON.stringify({
    subscription: {
      id: "00000000-0000-4000-8000-000000000000",
      type: "stream.online",
      version: "1",
      status: "enabled",
      created_at: new Date().toISOString(),
      condition: { broadcaster_user_id: "12345" },
      transport: { method: "webhook", callback: ingestURL },
    },
    event: {
      id: "synthetic-1",
      broadcaster_user_id: "12345",
      broadcaster_user_login: "test_channel",
      broadcaster_user_name: "Test Channel",
      type: "live",
      started_at: new Date().toISOString(),
    },
  });
}

class Subscriber {
  private ws!: WebSocket;
  private pending: Frame[] = [];
  private waiters: Array<(frame: Frame) => void> = [];
  private lastCursor = 0;

  async open(): Promise<void> {
    // Resume from cursor=Number.MAX_SAFE_INTEGER so the relay does not
    // replay anything it might be holding from earlier connections; we only
    // care about the events this script POSTs.
    const url =
      baseURL.replace(/^http/, "ws") +
      `/u/${token}/subscribe?cursor=${Number.MAX_SAFE_INTEGER}`;
    this.ws = new WebSocket(url);
    this.ws.addEventListener("message", (event) => {
      const frame = JSON.parse(event.data as string) as Frame;
      this.lastCursor = frame.cursor;
      const waiter = this.waiters.shift();
      if (waiter) waiter(frame);
      else this.pending.push(frame);
    });
    await new Promise<void>((resolve, reject) => {
      const timer = setTimeout(
        () => reject(new Error(`websocket connect timeout: ${url}`)),
        10_000,
      );
      this.ws.addEventListener("open", () => {
        clearTimeout(timer);
        resolve();
      });
      this.ws.addEventListener("error", (e) => {
        clearTimeout(timer);
        reject(new Error(`websocket error: ${(e as ErrorEvent).message ?? e}`));
      });
      this.ws.addEventListener("close", (e) => {
        clearTimeout(timer);
        const close = e as CloseEvent;
        reject(
          new Error(
            `websocket closed before open: ${close.code} ${close.reason}`,
          ),
        );
      });
    });
  }

  next(timeoutMs = 8_000): Promise<Frame> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(
        () => reject(new Error("timed out waiting for relay frame")),
        timeoutMs,
      );
      const queued = this.pending.shift();
      if (queued) {
        clearTimeout(timer);
        resolve(queued);
        return;
      }
      this.waiters.push((frame) => {
        clearTimeout(timer);
        resolve(frame);
      });
    });
  }

  send(payload: object): void {
    this.ws.send(JSON.stringify(payload));
  }

  close(): void {
    this.ws.close();
  }
}

async function asyncForwardCheck(sub: Subscriber): Promise<string> {
  const ingestPromise = fetch(ingestURL, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Twitch-Eventsub-Message-Id": "msg-async-1",
      "Twitch-Eventsub-Message-Type": "notification",
      "Twitch-Eventsub-Subscription-Type": "stream.online",
    },
    body: streamOnlinePayload(),
  });

  const frame = await sub.next();
  const ingestRes = await ingestPromise;
  if (ingestRes.status !== 202) {
    throw new Error(
      `ingest expected 202, got ${ingestRes.status} ${await ingestRes.text()}`,
    );
  }
  if (frame.requires_response) {
    throw new Error("expected requires_response=false on async delivery");
  }
  const decoded = decodeBase64(frame.body);
  if (!decoded.includes('"stream.online"')) {
    throw new Error(`frame body did not contain stream.online: ${decoded}`);
  }
  sub.send({
    type: "dispatch_result",
    id: frame.id,
    status: 204,
    headers: {},
    body: "",
  });
  return `async forward ok (cursor=${frame.cursor})`;
}

async function verificationChallengeCheck(sub: Subscriber): Promise<string> {
  const challenge = `synthetic-challenge-${Date.now()}`;
  const verificationBody = JSON.stringify({
    challenge,
    subscription: {
      id: "00000000-0000-4000-8000-000000000001",
      type: "stream.online",
      version: "1",
      status: "webhook_callback_verification_pending",
      condition: { broadcaster_user_id: "12345" },
      transport: { method: "webhook", callback: ingestURL },
      created_at: new Date().toISOString(),
    },
  });

  const ingestPromise = fetch(ingestURL, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Twitch-Eventsub-Message-Id": "msg-verify-1",
      "Twitch-Eventsub-Message-Type": "webhook_callback_verification",
      "Twitch-Eventsub-Subscription-Type": "stream.online",
    },
    body: verificationBody,
  });

  const frame = await sub.next();
  if (!frame.requires_response) {
    throw new Error(
      "expected requires_response=true on verification (got an unexpected frame; " +
        "the relay buffer may still hold older events)",
    );
  }
  sub.send({
    type: "dispatch_result",
    id: frame.id,
    status: 200,
    headers: { "content-type": "text/plain" },
    body: encodeBase64(challenge),
  });

  const ingestRes = await ingestPromise;
  if (ingestRes.status !== 200) {
    throw new Error(
      `verification expected 200, got ${ingestRes.status} ${await ingestRes.text()}`,
    );
  }
  const responseBody = await ingestRes.text();
  if (responseBody !== challenge) {
    throw new Error(
      `verification body mismatch: expected ${challenge}, got ${responseBody}`,
    );
  }
  return `verification challenge ok (cursor=${frame.cursor})`;
}

async function main() {
  console.log(`[relay verify] base=${baseURL}`);
  console.log(`[relay verify] token=${token!.slice(0, 6)}…${token!.slice(-4)}`);
  const sub = new Subscriber();
  await sub.open();
  let pass = 0;
  let fail = 0;
  try {
    for (const [name, fn] of [
      ["async forward", asyncForwardCheck],
      ["verification challenge", verificationChallengeCheck],
    ] as const) {
      try {
        const detail = await fn(sub);
        console.log(`PASS  ${name}  ${detail}`);
        pass++;
      } catch (err) {
        console.error(
          `FAIL  ${name}  ${err instanceof Error ? err.message : String(err)}`,
        );
        fail++;
      }
    }
  } finally {
    sub.close();
  }
  console.log(`\n${pass} passed, ${fail} failed`);
  process.exit(fail === 0 ? 0 : 1);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
