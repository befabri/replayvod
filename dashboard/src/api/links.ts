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

export const KEEPALIVE_CONTEXT = { keepalive: true } as const;

export function isKeepaliveOp(op: Operation): boolean {
	return op.type === "mutation" && op.context.keepalive === true;
}

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
