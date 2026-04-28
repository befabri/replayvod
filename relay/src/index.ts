import { Hono } from "hono";

export { RelayDO } from "./relay-do";

const TOKEN_PATTERN = /^[a-zA-Z0-9_-]{16,128}$/;
const DEFAULT_TOKEN_VALIDATE_TIMEOUT_MS = 3_000;
const MAX_TOKEN_VALIDATE_TIMEOUT_MS = 10_000;

type Bindings = CloudflareBindings & {
  TOKEN_VALIDATE_URL?: string;
  RELAY_SHARED_SECRET?: string;
  TOKEN_VALIDATE_TIMEOUT_MS?: string;
  ALLOW_INSECURE_TOKEN_VALIDATE_URL?: string;
};

const app = new Hono<{ Bindings: Bindings }>();

app.get("/", (c) => c.text("replayvod relay\n"));
app.get("/health", (c) => c.text("ok\n"));

app.post("/u/:token", async (c) =>
  forward(c.env, c.req.param("token"), c.req.raw),
);

app.get("/u/:token/subscribe", async (c) => {
  if (c.req.header("Upgrade")?.toLowerCase() !== "websocket") {
    return c.text("expected websocket upgrade", 426);
  }
  return forward(c.env, c.req.param("token"), c.req.raw);
});

async function forward(env: Bindings, token: string, request: Request) {
  if (!TOKEN_PATTERN.test(token)) {
    return new Response("invalid token\n", { status: 400 });
  }
  const auth = await validateToken(env, token);
  if (!auth.ok) {
    return new Response(auth.message, { status: auth.status });
  }
  const id = env.RELAY.idFromName(token);
  const stub = env.RELAY.get(id);
  return stub.fetch(request);
}

async function validateToken(
  env: Bindings,
  token: string,
): Promise<{ ok: true } | { ok: false; status: number; message: string }> {
  if (!env.TOKEN_VALIDATE_URL && !env.RELAY_SHARED_SECRET) {
    return { ok: true };
  }
  if (!env.TOKEN_VALIDATE_URL || !env.RELAY_SHARED_SECRET) {
    return {
      ok: false,
      status: 503,
      message: "relay token validation not configured\n",
    };
  }
  if (!isAllowedTokenValidateURL(env)) {
    return {
      ok: false,
      status: 503,
      message: "relay token validation URL must use https\n",
    };
  }

  let res: Response;
  let body: { valid?: boolean };
  const timeoutMs = tokenValidateTimeoutMs(env);
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    res = await fetch(env.TOKEN_VALIDATE_URL, {
      method: "POST",
      signal: controller.signal,
      headers: {
        Authorization: `Bearer ${env.RELAY_SHARED_SECRET}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ token }),
    });
    if (!res.ok) {
      return {
        ok: false,
        status: res.status >= 500 ? 503 : 403,
        message: "relay token rejected\n",
      };
    }
    body = await res.json<{ valid?: boolean }>().catch((err) => {
      if (controller.signal.aborted) throw err;
      return { valid: false };
    });
  } catch {
    return {
      ok: false,
      status: 503,
      message: "relay token validation unavailable\n",
    };
  } finally {
    clearTimeout(timeout);
  }

  if (!body.valid) {
    return { ok: false, status: 403, message: "relay token rejected\n" };
  }
  return { ok: true };
}

function tokenValidateTimeoutMs(env: Bindings): number {
  const configured = Number(env.TOKEN_VALIDATE_TIMEOUT_MS);
  if (Number.isFinite(configured) && configured > 0) {
    return Math.min(configured, MAX_TOKEN_VALIDATE_TIMEOUT_MS);
  }
  return DEFAULT_TOKEN_VALIDATE_TIMEOUT_MS;
}

function isAllowedTokenValidateURL(env: Bindings): boolean {
  if (!env.TOKEN_VALIDATE_URL) return false;
  try {
    const url = new URL(env.TOKEN_VALIDATE_URL);
    if (url.protocol === "https:") return true;
    return env.ALLOW_INSECURE_TOKEN_VALIDATE_URL === "1";
  } catch {
    return false;
  }
}

export default app;
