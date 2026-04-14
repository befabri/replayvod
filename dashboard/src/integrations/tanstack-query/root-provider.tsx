import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createTRPCClient,
	httpBatchLink,
	httpSubscriptionLink,
	splitLink,
} from "@trpc/client";
import type { ReactNode } from "react";
import { type AppRouter, TRPCProvider } from "@/api/trpc";
import { API_URL } from "@/env";

// splitLink routes subscription ops (task.status, stream.live,
// system.events, video.downloadProgress) through httpSubscriptionLink
// which speaks Server-Sent Events; everything else keeps using
// httpBatchLink for the existing query + mutation path. The SSE path
// uses EventSource, which carries the session cookie automatically as
// long as the dashboard is served same-origin; the explicit
// credentials fetch override on the batch link keeps parity with our
// cross-origin dev flow.
export const trpcClient = createTRPCClient<AppRouter>({
	links: [
		splitLink({
			condition: (op) => op.type === "subscription",
			true: httpSubscriptionLink({
				url: `${API_URL}/trpc`,
				eventSourceOptions: { withCredentials: true },
			}),
			false: httpBatchLink({
				url: `${API_URL}/trpc`,
				fetch(url, options) {
					return fetch(url, { ...options, credentials: "include" });
				},
			}),
		}),
	],
});

export function getContext() {
	const queryClient = new QueryClient({
		defaultOptions: {
			queries: {
				gcTime: 1000 * 60 * 5,
				staleTime: 1000 * 30,
			},
		},
	});

	return { queryClient };
}

export default function TanstackQueryProvider({
	children,
	context,
}: {
	children: ReactNode;
	context: ReturnType<typeof getContext>;
}) {
	const { queryClient } = context;

	return (
		<QueryClientProvider client={queryClient}>
			<TRPCProvider trpcClient={trpcClient} queryClient={queryClient}>
				{children}
			</TRPCProvider>
		</QueryClientProvider>
	);
}
