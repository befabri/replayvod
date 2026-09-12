import {
	createWSClient,
	httpBatchLink,
	httpLink,
	type Operation,
	splitLink,
	type TRPCLink,
	wsLink,
} from "@trpc/client";
import { credentialTransportLink } from "@/api/credential-transport";
import type { AppRouter } from "@/api/trpc";

// KEEPALIVE_CONTEXT sends a mutation on a keepalive request, which the browser
// lets finish after the page unloads. It is set per mutation rather than per
// call, so a write that can fire from pagehide carries it on every call, not
// only the unload one: the watch progress writer sets it, so its throttled
// mid-playback saves go out unbatched and keepalive too. That is the point.
// Which save turns out to be the last one is not knowable in advance, and the
// payloads are far inside the browser's 64 KiB in-flight keepalive budget.
export const KEEPALIVE_CONTEXT = { keepalive: true } as const;

export function isKeepaliveOp(op: Operation): boolean {
	return op.type === "mutation" && op.context.keepalive === true;
}

// dashboardLinks composes the dashboard transport. Subscriptions speak
// a shared WebSocket; keepalive mutations (see KEEPALIVE_CONTEXT) go out one
// per request so the flag applies to exactly that write and a batch-mate
// cannot hold it back; everything else batches. `credentials: "include"` keeps
// the session cookie on the cross-origin dev flow. WebSocket handshakes carry
// the same cookies and the server checks their Origin before accepting them.
export function dashboardLinks({
	apiUrl,
	credentialsAllowed,
	fetch: fetchOverride,
}: {
	apiUrl: string;
	credentialsAllowed: () => boolean;
	fetch?: typeof globalThis.fetch;
}): TRPCLink<AppRouter>[] {
	const url = `${apiUrl}/trpc`;
	// Resolved per request so a fetch installed later (instrumentation, a
	// polyfill) is honoured; binding at startup would pin the original.
	const fetchImpl: typeof globalThis.fetch = (input, init) =>
		(fetchOverride ?? globalThis.fetch)(input, { ...init, redirect: "error" });
	return [
		credentialTransportLink(credentialsAllowed),
		splitLink({
			condition: isKeepaliveOp,
			true: httpLink({
				url,
				fetch(input, options) {
					return fetchImpl(input, {
						...options,
						credentials: "include",
						keepalive: true,
					});
				},
			}),
			false: splitLink({
				condition: (op) => op.type === "subscription",
				true: subscriptionLink(apiUrl),
				false: httpBatchLink({
					url,
					fetch(input, options) {
						return fetchImpl(input, { ...options, credentials: "include" });
					},
				}),
			}),
		}),
	];
}

// Initialize on the first subscription, never during SSR or an HTTP-only call.
// wsLink multiplexes all observers through this client; reconnect restores the
// active subscriptions and onStarted lets their query caches resynchronize.
function subscriptionLink(apiUrl: string): TRPCLink<AppRouter> {
	return (runtime) => {
		let link: ReturnType<TRPCLink<AppRouter>> | undefined;
		return (options) => {
			if (!link) {
				const client = createWSClient({
					url: () => subscriptionUrl(apiUrl, window.location.href),
					lazy: { enabled: true, closeMs: 1_000 },
					keepAlive: {
						enabled: true,
						intervalMs: 25_000,
						pongTimeoutMs: 10_000,
					},
				});
				link = wsLink<AppRouter>({ client })(runtime);
			}
			return link(options);
		};
	};
}

export function subscriptionUrl(apiUrl: string, pageUrl: string): string {
	const url = new URL(`${apiUrl}/trpc/ws`, pageUrl);
	url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
	return url.href;
}
