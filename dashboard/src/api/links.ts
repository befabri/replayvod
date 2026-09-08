import {
	httpBatchLink,
	httpLink,
	httpSubscriptionLink,
	type Operation,
	splitLink,
	type TRPCLink,
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
// Server-Sent Events; keepalive mutations (see KEEPALIVE_CONTEXT) go out one
// per request so the flag applies to exactly that write and a batch-mate
// cannot hold it back; everything else batches. `credentials: "include"` keeps
// the session cookie on the cross-origin dev flow, and EventSource carries it
// on its own when served same-origin.
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
				true: httpSubscriptionLink({
					url,
					eventSourceOptions: { withCredentials: true },
				}),
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
