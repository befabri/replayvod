import type { ReactNode } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { createTRPCClient, httpBatchLink } from "@trpc/client"
import { TRPCProvider, type AppRouter } from "@/api/trpc"
import { API_URL } from "@/env"

export const trpcClient = createTRPCClient<AppRouter>({
	links: [
		httpBatchLink({
			url: `${API_URL}/trpc`,
			fetch(url, options) {
				return fetch(url, { ...options, credentials: "include" })
			},
		}),
	],
})

export function getContext() {
	const queryClient = new QueryClient({
		defaultOptions: {
			queries: {
				gcTime: 1000 * 60 * 5,
				staleTime: 1000 * 30,
			},
		},
	})

	return { queryClient }
}

export default function TanstackQueryProvider({
	children,
	context,
}: {
	children: ReactNode
	context: ReturnType<typeof getContext>
}) {
	const { queryClient } = context

	return (
		<QueryClientProvider client={queryClient}>
			<TRPCProvider trpcClient={trpcClient} queryClient={queryClient}>
				{children}
			</TRPCProvider>
		</QueryClientProvider>
	)
}
