import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import { useEffect } from "react";
import { Navbar } from "@/components/layout/navbar";
import {
	SIDEBAR_EASE,
	SIDEBAR_MARGIN_COLLAPSED,
	SIDEBAR_MARGIN_EXPANDED,
	Sidebar,
} from "@/components/layout/sidebar";
import { RouteError } from "@/components/route-error";
import { LoadingState } from "@/components/ui/loading-state";
import { useKeepSettingsLoaded } from "@/features/settings/queries";
import { StorageBanner } from "@/features/storage/components/StorageBanner";
import { useLiveStreamStatus } from "@/features/streams-live/queries";
import { useLiveVideoChanges } from "@/features/videos/queries";
import { cn } from "@/lib/utils";
import { resolveSession, setUser } from "@/stores/auth";
import { uiStore } from "@/stores/ui";

export const Route = createFileRoute("/dashboard")({
	beforeLoad: async () => {
		const user = await resolveSession();
		if (!user) throw redirect({ to: "/login", search: { error: undefined } });
		return { user };
	},
	component: DashboardLayout,
	pendingComponent: DashboardPending,
	errorComponent: RouteError,
});

function DashboardPending() {
	return (
		<div className="flex min-h-screen items-center justify-center">
			<LoadingState />
		</div>
	);
}

function DashboardLayout() {
	useLiveStreamStatus();
	useLiveVideoChanges();
	useKeepSettingsLoaded();
	const { user } = Route.useRouteContext();
	const collapsed = useSelector(uiStore, (s) => s.sidebarCollapsed);

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
				<StorageBanner />
				<Outlet />
			</main>
		</div>
	);
}
