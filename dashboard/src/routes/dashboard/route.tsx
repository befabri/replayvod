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
import { RouteError } from "@/components/route-error";
import { PlaybackSettingsProvider } from "@/features/settings/playback";
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
	const { t } = useTranslation();
	return (
		<div className="flex min-h-screen items-center justify-center">
			<div className="text-muted-foreground">{t("common.loading")}</div>
		</div>
	);
}

function DashboardLayout() {
	useLiveStreamStatus();
	useLiveVideoChanges();
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
				<PlaybackSettingsProvider>
					<Outlet />
				</PlaybackSettingsProvider>
			</main>
		</div>
	);
}
