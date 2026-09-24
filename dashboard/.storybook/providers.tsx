import type { Decorator } from "@storybook/react-vite";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRootRoute,
	createRoute,
	createRouter,
	RouterProvider,
} from "@tanstack/react-router";
import { createTRPCOptionsProxy } from "@trpc/tanstack-react-query";
import {
	createContext,
	type ReactNode,
	useContext,
	useLayoutEffect,
	useMemo,
	useState,
} from "react";
import { type AppRouter, TRPCProvider } from "@/api/trpc";
import { Toaster } from "@/components/ui/toaster";
import i18n from "@/i18n";
import { clearUser, isRole, setUser } from "@/stores/auth";
import { setTheme } from "@/stores/theme";
import { CURRENT_USER, USER_SETTINGS } from "@/test/fixtures";
import {
	createMockTrpcClient,
	mergeTrpcHandlers,
	type TrpcHandlers,
} from "@/test/trpc-mock";

const StoryContent = createContext<ReactNode>(null);

function StoryOutlet() {
	return useContext(StoryContent);
}

const DEFAULT_HANDLERS: TrpcHandlers = {
	settings: { get: () => USER_SETTINGS },
};

const NO_HANDLERS: TrpcHandlers = {};

function createStoryRouter(initialPath: string) {
	const rootRoute = createRootRoute({ component: StoryOutlet });
	return createRouter({
		routeTree: rootRoute.addChildren([
			createRoute({ getParentRoute: () => rootRoute, path: "$" }),
		]),
		history: createMemoryHistory({ initialEntries: [initialPath] }),
	});
}

function createStoryQueryClient(handlers: TrpcHandlers) {
	const queryClient = new QueryClient({
		defaultOptions: {
			queries: {
				retry: false,
				staleTime: Number.POSITIVE_INFINITY,
				refetchOnWindowFocus: false,
			},
			mutations: { retry: false },
		},
	});
	if (!handlers.settings?.get) {
		const trpc = createTRPCOptionsProxy<AppRouter>({
			client: createMockTrpcClient(NO_HANDLERS),
			queryClient,
		});
		queryClient.setQueryData(trpc.settings.get.queryKey(), USER_SETTINGS);
	}
	return queryClient;
}

function applyGlobals(theme: string, locale: string, role: string) {
	setTheme(theme === "light" ? "light" : "dark");
	if (i18n.language !== locale) void i18n.changeLanguage(locale);
	document.documentElement.lang = locale;
	if (isRole(role)) {
		setUser({ ...CURRENT_USER, role });
	} else {
		clearUser();
	}
}

function StoryProviders({
	theme,
	locale,
	role,
	handlers,
	initialPath,
	children,
}: {
	theme: string;
	locale: string;
	role: string;
	handlers: TrpcHandlers;
	initialPath: string;
	children: ReactNode;
}) {
	const [queryClient] = useState(() => createStoryQueryClient(handlers));
	const [router] = useState(() => createStoryRouter(initialPath));
	const trpcClient = useMemo(
		() => createMockTrpcClient(mergeTrpcHandlers(DEFAULT_HANDLERS, handlers)),
		[handlers],
	);
	const [ready, setReady] = useState(false);

	useLayoutEffect(() => {
		applyGlobals(theme, locale, role);
		setReady(true);
	}, [theme, locale, role]);

	if (!ready) return null;

	return (
		<QueryClientProvider client={queryClient}>
			<TRPCProvider trpcClient={trpcClient} queryClient={queryClient}>
				<StoryContent.Provider value={children}>
					<RouterProvider router={router} />
				</StoryContent.Provider>
				<Toaster />
			</TRPCProvider>
		</QueryClientProvider>
	);
}

export const withProviders: Decorator = (Story, { globals, parameters }) => (
	<StoryProviders
		theme={globals.theme}
		locale={globals.locale}
		role={globals.role}
		handlers={parameters.trpc ?? NO_HANDLERS}
		initialPath={parameters.initialPath ?? "/"}
	>
		<Story />
	</StoryProviders>
);
