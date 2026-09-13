import type { Page } from "@playwright/test";
import { USER_SETTINGS } from "../../src/test/playback-settings";

// A valid session payload shaped like the server's auth.session output.
export const SESSION = {
	user_id: "u1",
	login: "alice",
	display_name: "Alice",
	email: "alice@example.com",
	profile_image_url: "",
	role: "owner",
};

// procsOf parses the batched procedure names out of a /trpc request URL.
// httpBatchLink encodes the batch as a comma-separated path: /trpc/a.b,c.d
export function procsOf(url: string): string[] {
	const path = new URL(url).pathname.replace(/^.*\/trpc\//, "");
	return path.split(",").map((p) => decodeURIComponent(p));
}

export function trpcOk(values: unknown[]) {
	return values.map((data) => ({ result: { data } }));
}

export function trpcUnauthorized(procs: string[]) {
	return procs.map(() => ({
		error: {
			message: "UNAUTHORIZED",
			code: -32001,
			data: { code: "UNAUTHORIZED", httpStatus: 401 },
		},
	}));
}

export type TrpcResolver = (
	procs: string[],
	url: string,
) => { status: number; body: unknown } | null;

// mockTrpc intercepts the batched tRPC endpoint. `resolve` returns a
// { status, body } to fulfill, or null to fall through to a default success
// (empty video pages, default settings, and null for other procedures). SSE
// subscription requests (Accept: text/event-stream) are aborted so EventSource
// retries against the mock instead of reaching a real server.
export async function mockTrpc(page: Page, resolve: TrpcResolver) {
	await page.route("**/trpc/**", async (route) => {
		const req = route.request();
		if ((req.headers().accept ?? "").includes("text/event-stream")) {
			await route.abort();
			return;
		}
		const procs = procsOf(req.url());
		const custom = resolve(procs, req.url());
		const { status, body } = custom ?? {
			status: 200,
			body: trpcOk(
				procs.map((proc) => (proc === "video.listPage" ? { items: [] } : null)),
			),
		};
		await route.fulfill({
			status,
			contentType: "application/json",
			body: JSON.stringify(
				Array.isArray(body)
					? body.map((result, index) =>
							procs[index] === "settings.get" &&
							result?.result?.data == null &&
							result?.result
								? { result: { data: USER_SETTINGS } }
								: result,
						)
					: body,
			),
		});
	});
}

// validSession authenticates auth.session and lets everything else fall through
// to the default. Useful as the base for "logged in" specs.
export const validSession: TrpcResolver = (procs) =>
	procs.includes("auth.session")
		? {
				status: 200,
				body: trpcOk(procs.map((p) => (p === "auth.session" ? SESSION : null))),
			}
		: null;

// Exercise subscription-driven cache refreshes alongside mocked HTTP snapshots.
// The returned publisher broadcasts one procedure's data to every live observer.
export async function mockSubscription(page: Page, path: string) {
	const subscribers = new Map<number | string, (data: unknown) => void>();
	await page.routeWebSocket("**/trpc/ws", (socket) => {
		socket.onMessage((raw) => {
			const text = String(raw);
			if (text === "PING") {
				socket.send("PONG");
				return;
			}
			const parsed = JSON.parse(text);
			for (const message of Array.isArray(parsed) ? parsed : [parsed]) {
				if (message.method === "subscription.stop") {
					subscribers.delete(message.id);
					continue;
				}
				if (message.params?.path === path) {
					subscribers.set(message.id, (data) =>
						socket.send(
							JSON.stringify({
								id: message.id,
								result: { type: "data", data },
							}),
						),
					);
					socket.send(
						JSON.stringify({ id: message.id, result: { type: "started" } }),
					);
				}
			}
		});
		socket.onClose(() => subscribers.clear());
	});
	return (data: unknown = {}) => {
		for (const send of subscribers.values()) send(data);
	};
}
