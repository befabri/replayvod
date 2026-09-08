import {
	httpBatchLink,
	httpSubscriptionLink,
	splitLink,
	type TRPCLink,
} from "@trpc/client";
import { credentialTransportLink } from "@/api/credential-transport";
import type { AppRouter } from "@/api/trpc";

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
	return [
		credentialTransportLink(credentialsAllowed),
		splitLink({
			condition: (op) => op.type === "subscription",
			true: httpSubscriptionLink({
				url,
				eventSourceOptions: { withCredentials: true },
			}),
			false: httpBatchLink({
				url,
				fetch(input, options) {
					return (fetchOverride ?? globalThis.fetch)(input, {
						...options,
						credentials: "include",
						redirect: "error",
					});
				},
			}),
		}),
	];
}
