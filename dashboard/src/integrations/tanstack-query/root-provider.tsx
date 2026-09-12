import {
	MutationCache,
	QueryCache,
	QueryClient,
	QueryClientProvider,
} from "@tanstack/react-query";
import { createTRPCClient } from "@trpc/client";
import type { ReactNode } from "react";
import { browserCredentialTransportAllowed } from "@/api/credential-transport";
import { dashboardLinks } from "@/api/links";
import { type AppRouter, TRPCProvider } from "@/api/trpc";
import { handleApiError, isUnauthorized } from "@/api/unauthorized";
import { API_URL } from "@/env";

// The link chain lives in dashboardLinks: subscriptions (task.status,
// stream.live, system.events, video.downloadProgress) share one WebSocket;
// keepalive writes use their own request, with batching for the rest.
export const trpcClient = createTRPCClient<AppRouter>({
	links: dashboardLinks({
		apiUrl: API_URL,
		credentialsAllowed: () => browserCredentialTransportAllowed(API_URL),
	}),
});

export function getContext() {
	const queryClient = new QueryClient({
		// A 401 from any query or mutation means the session expired; route it
		// through the shared handler so a single redirect fires (see
		// handleApiError). The caches are the only global error seam, since
		// per-call onError handlers don't see requests that never opted in.
		queryCache: new QueryCache({ onError: handleApiError }),
		mutationCache: new MutationCache({ onError: handleApiError }),
		defaultOptions: {
			queries: {
				gcTime: 1000 * 60 * 5,
				staleTime: 1000 * 30,
				// Never retry a 401: the session won't un-expire, so retrying just
				// delays the redirect (and hammers the server). Keep the default
				// retry budget for everything else.
				retry: (failureCount, error) =>
					!isUnauthorized(error) && failureCount < 3,
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
