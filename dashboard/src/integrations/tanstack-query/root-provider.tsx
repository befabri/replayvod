import {
	MutationCache,
	QueryCache,
	QueryClient,
	QueryClientProvider,
} from "@tanstack/react-query";
import { createTRPCClient } from "@trpc/client";
import { createTRPCOptionsProxy } from "@trpc/tanstack-react-query";
import type { ReactNode } from "react";
import { browserCredentialTransportAllowed } from "@/api/credential-transport";
import { dashboardLinks } from "@/api/links";
import { type AppRouter, TRPCProvider } from "@/api/trpc";
import { handleApiError, isUnauthorized } from "@/api/unauthorized";
import { API_URL } from "@/env";

export const trpcClient = createTRPCClient<AppRouter>({
	links: dashboardLinks({
		apiUrl: API_URL,
		credentialsAllowed: () => browserCredentialTransportAllowed(API_URL),
	}),
});

export function getContext() {
	const queryClient = new QueryClient({
		queryCache: new QueryCache({ onError: handleApiError }),
		mutationCache: new MutationCache({ onError: handleApiError }),
		defaultOptions: {
			queries: {
				gcTime: 1000 * 60 * 5,
				staleTime: 1000 * 30,
				retry: (failureCount, error) =>
					!isUnauthorized(error) && failureCount < 3,
			},
		},
	});

	const trpc = createTRPCOptionsProxy<AppRouter>({
		client: trpcClient,
		queryClient,
	});

	return { queryClient, trpc };
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
