import { createRouter as createTanStackRouter } from "@tanstack/react-router";
import { setupRouterSsrQueryIntegration } from "@tanstack/react-router-ssr-query";
import type { ReactNode } from "react";
import { registerUnauthorizedRedirect } from "./api/unauthorized";
import TanstackQueryProvider, {
	getContext,
} from "./integrations/tanstack-query/root-provider";
import { routeTree } from "./routeTree.gen";
import { clearUser } from "./stores/auth";

export function getRouter() {
	const context = getContext();

	const router = createTanStackRouter({
		routeTree,
		context,
		scrollRestoration: true,
		defaultPreload: "intent",
		defaultPreloadStaleTime: 0,

		Wrap: (props: { children: ReactNode }) => {
			return (
				<TanstackQueryProvider context={context}>
					{props.children}
				</TanstackQueryProvider>
			);
		},
	});

	setupRouterSsrQueryIntegration({ router, queryClient: context.queryClient });

	registerUnauthorizedRedirect(() => {
		clearUser();
		void router.navigate({ to: "/login", search: { error: undefined } });
	});

	return router;
}

declare module "@tanstack/react-router" {
	interface Register {
		router: ReturnType<typeof getRouter>;
	}
}
