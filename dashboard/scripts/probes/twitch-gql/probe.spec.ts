import { test, expect, type Request, type Response } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Targets the hosts the download pipeline cares about. Everything
// else (static.twitchcdn, countess metrics, etc.) is noise.
const HOSTS_OF_INTEREST = [
	"gql.twitch.tv",
	"usher.ttvnw.net",
	"video-edge", // the edge CDN that serves HLS segments
	"video-weaver",
];

const CHANNEL = process.env.PROBE_CHANNEL ?? "tumblurr";
const OUT_FILE = path.join(__dirname, `capture.${CHANNEL}.json`);
const WATCH_MS = 15_000; // give the player time to fetch master + a few segments

// Keys, URL query params, and headers in the captured payload that
// carry PII (IP), pseudo-identifiers (device_id, play_session_id),
// or short-lived secrets (PlaybackAccessToken signatures, auth
// headers) we don't want landing in a committable artifact. Scrubbed
// as a post-serialization string pass — exhaustive enough for any
// JSON structure this probe produces, cheap to audit.
//
// Note: Twitch embeds the PlaybackAccessToken body as an
// escape-stringified JSON inside the `value` field of the outer
// response. That means `user_ip` shows up twice in the final bytes:
// once as `"user_ip":"..."` (top-level) and once as `\"user_ip\":\"...\"`
// (inside the escaped string). We scrub both forms.
const REDACTED = "[redacted]";
const SENSITIVE_KEYS = [
	"user_ip",
	"device_id",
	"play_session_id",
	"signature",
	"authorization",
	"cookie",
	"set-cookie",
	"client-integrity",
	"x-device-id",
];
const JSON_KEY_PATTERNS: RegExp[] = SENSITIVE_KEYS.flatMap((key) => [
	// Unescaped form: "key":"value"
	new RegExp(`"${key}"\\s*:\\s*"[^"]*"`, "gi"),
	// Escaped form (inside a stringified JSON value): \"key\":\"value\"
	new RegExp(`\\\\"${key}\\\\"\\s*:\\s*\\\\"[^"\\\\]*\\\\"`, "gi"),
]);
const URL_PARAM_PATTERNS: RegExp[] = [
	/([?&])sig=[^&"\s]+/g,
	/([?&])play_session_id=[^&"\s]+/g,
	/([?&])token=[^&"\s]+/g,
	/([?&])device_id=[^&"\s]+/g,
];
function scrub(serialized: string): string {
	let out = serialized;
	for (const re of JSON_KEY_PATTERNS) {
		out = out.replace(re, (m) => {
			// Replace the LAST quoted (or escape-quoted) value in the
			// match, preserving the key + punctuation.
			return m
				.replace(/\\"[^"\\]*\\"$/, `\\"${REDACTED}\\"`)
				.replace(/"[^"]*"$/, `"${REDACTED}"`);
		});
	}
	for (const re of URL_PARAM_PATTERNS) {
		out = out.replace(re, (_m, sep) => `${sep}${REDACTED}`);
	}
	return out;
}

type CapturedEvent = {
	ts: number;
	phase: "request" | "response";
	method?: string;
	url: string;
	status?: number;
	headers: Record<string, string>;
	body?: unknown;
	bodyText?: string;
	bodyBytes?: number;
	note?: string;
};

function hostMatches(url: string) {
	return HOSTS_OF_INTEREST.some((h) => url.includes(h));
}

// Strip noise from header dumps so the diff against our Go client is readable.
// `cf-*`, `x-served-by`, CDN cache hints etc. are informative but crowd the view.
const DROP_HEADER_PREFIXES = ["cf-", "x-served-by", "x-cache", "via", "age", "strict-transport", "alt-svc", "report-to", "nel", "server-timing"];
function cleanHeaders(h: Record<string, string>): Record<string, string> {
	const out: Record<string, string> = {};
	for (const [k, v] of Object.entries(h)) {
		const lk = k.toLowerCase();
		if (DROP_HEADER_PREFIXES.some((p) => lk.startsWith(p))) continue;
		out[lk] = v;
	}
	return out;
}

function shortUrl(u: string) {
	try {
		const url = new URL(u);
		return `${url.host}${url.pathname}`;
	} catch {
		return u;
	}
}

async function captureRequest(req: Request): Promise<CapturedEvent> {
	const raw = req.postData();
	let body: unknown = undefined;
	let bodyText: string | undefined = raw ?? undefined;
	if (raw) {
		try {
			body = JSON.parse(raw);
			bodyText = undefined;
		} catch {
			// not JSON, keep the raw text
		}
	}
	return {
		ts: Date.now(),
		phase: "request",
		method: req.method(),
		url: req.url(),
		headers: cleanHeaders(await req.allHeaders()),
		body,
		bodyText,
	};
}

async function captureResponse(resp: Response): Promise<CapturedEvent> {
	const url = resp.url();
	let body: unknown = undefined;
	let bodyText: string | undefined;
	let bodyBytes: number | undefined;
	let note: string | undefined;
	try {
		const buf = await resp.body();
		bodyBytes = buf.byteLength;
		const text = buf.toString("utf8");
		const ct = resp.headers()["content-type"] ?? "";
		if (ct.includes("json")) {
			try {
				body = JSON.parse(text);
			} catch {
				bodyText = text.slice(0, 4000);
			}
		} else if (ct.includes("mpegurl") || url.endsWith(".m3u8")) {
			// Keep full manifest — that's the point of probing.
			bodyText = text;
		} else if (ct.startsWith("text") || ct.includes("xml")) {
			bodyText = text.slice(0, 4000);
		} else {
			note = `binary body, ${bodyBytes} bytes`;
		}
	} catch (err) {
		note = `body unavailable: ${(err as Error).message}`;
	}
	return {
		ts: Date.now(),
		phase: "response",
		url,
		status: resp.status(),
		headers: cleanHeaders(await resp.allHeaders()),
		body,
		bodyText,
		bodyBytes,
		note,
	};
}

test.describe.configure({ mode: "serial" });

test(`probe GQL flow on twitch.tv/${CHANNEL}`, async ({ browser }) => {
	test.setTimeout(WATCH_MS + 30_000);

	// Fresh context = no cookies, no local storage — the "anonymous
	// first visit" case the downloader mimics.
	const context = await browser.newContext({
		storageState: undefined,
		userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
		locale: "en-US",
	});
	const page = await context.newPage();

	const events: CapturedEvent[] = [];

	page.on("request", async (req) => {
		if (!hostMatches(req.url())) return;
		try {
			events.push(await captureRequest(req));
		} catch (err) {
			console.warn("capture request failed", req.url(), err);
		}
	});

	page.on("response", async (resp) => {
		if (!hostMatches(resp.url())) return;
		try {
			events.push(await captureResponse(resp));
		} catch (err) {
			console.warn("capture response failed", resp.url(), err);
		}
	});

	await page.goto(`https://www.twitch.tv/${CHANNEL}`, { waitUntil: "domcontentloaded" });

	// Let the player bootstrap — PlaybackAccessToken, usher master,
	// media playlist, first few segments.
	await page.waitForTimeout(WATCH_MS);

	await context.close();

	// Sort by timestamp so request/response pairs land next to each
	// other in the output.
	events.sort((a, b) => a.ts - b.ts);

	// --- Terminal summary ---
	console.log(`\n=== waterfall (${events.length} events, ${HOSTS_OF_INTEREST.join(" | ")}) ===\n`);
	for (const e of events) {
		const tag = e.phase === "request" ? ">>" : "<<";
		const head = e.phase === "request" ? `${e.method} ${shortUrl(e.url)}` : `${e.status} ${shortUrl(e.url)}${e.bodyBytes != null ? ` (${e.bodyBytes}B)` : ""}`;
		console.log(`${tag} ${head}`);
	}

	// --- Focused extract: every PlaybackAccessToken call ---
	const pats = events.filter((e) => {
		if (!e.url.includes("gql.twitch.tv/gql")) return false;
		const b = e.body as { operationName?: string } | Array<{ operationName?: string }> | undefined;
		if (!b) return false;
		if (Array.isArray(b)) return b.some((op) => op.operationName === "PlaybackAccessToken");
		return b.operationName === "PlaybackAccessToken";
	});
	const integrity = events.filter((e) => e.url.includes("gql.twitch.tv/integrity"));
	const usher = events.filter((e) => e.url.includes("usher.ttvnw.net"));

	console.log("\n=== PlaybackAccessToken ===");
	console.log(JSON.stringify(pats, null, 2));
	console.log("\n=== /integrity ===");
	console.log(JSON.stringify(integrity, null, 2));
	console.log("\n=== usher.ttvnw.net (master playlist) ===");
	console.log(JSON.stringify(usher, null, 2));

	// Scrub before write: the raw JSON has user IP, device_id,
	// PlaybackAccessToken signatures, and token/sig query params. None
	// of that belongs in a file we might accidentally commit.
	const raw = JSON.stringify(
		{ channel: CHANNEL, capturedAt: new Date().toISOString(), events },
		null,
		2,
	);
	fs.writeFileSync(OUT_FILE, scrub(raw));
	console.log(`\n=> full waterfall written to ${OUT_FILE} (PII scrubbed)`);

	// Sanity: we should have seen at least one PlaybackAccessToken
	// request. If not, the channel page didn't load the player or
	// Twitch changed the flow.
	expect(pats.length).toBeGreaterThan(0);
});
