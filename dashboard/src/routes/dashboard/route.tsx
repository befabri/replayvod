import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Navbar } from "@/components/layout/navbar";
import {
	SIDEBAR_EASE,
	SIDEBAR_MARGIN_COLLAPSED,
	SIDEBAR_MARGIN_EXPANDED,
	Sidebar,
} from "@/components/layout/sidebar";
import { cn } from "@/lib/utils";
import { resolveSession, setUser } from "@/stores/auth";
import { uiStore } from "@/stores/ui";

export const Route = createFileRoute("/dashboard")({
	// Resolve the session before the protected layout renders. An unauthenticated
	// visitor is redirected here, in the loader phase, so no protected chrome ever
	// paints. The guard stays pure (it does not touch authStore); the resolved
	// user rides the route context and hydrates the store after mount below.
	beforeLoad: async () => {
		const user = await resolveSession();
		if (!user) throw redirect({ to: "/login", search: { error: undefined } });
		return { user };
	},
	component: DashboardLayout,
	pendingComponent: DashboardPending,
});

function DashboardPending() {
	const { t } = useTranslation();
	return (
		<div className="flex min-h-screen items-center justify-center">
			<div className="text-muted-foreground">{t("common.loading")}</div>
		</div>
	);
}

function DashboardLayout() {
	const { user } = Route.useRouteContext();
	const collapsed = useSelector(uiStore, (s) => s.sidebarCollapsed);

	// Hydrate the auth store from the route-resolved session after mount. Kept
	// out of the beforeLoad guard so the store (which Navbar/Sidebar subscribe
	// to) isn't mutated mid-navigation, which previously produced
	// update-before-mount and hydration warnings.
	useEffect(() => {
		setUser(user);
	}, [user]);

	return (
		<div className="min-h-screen bg-background text-foreground">
			<Navbar />
			<Sidebar />
			<main
				className={cn(
					"mt-16 p-4 md:p-7 mb-4 transition-[margin] duration-300",
					SIDEBAR_EASE,
					collapsed ? SIDEBAR_MARGIN_COLLAPSED : SIDEBAR_MARGIN_EXPANDED,
				)}
			>
				<Outlet />
			</main>
		</div>
	);
}
