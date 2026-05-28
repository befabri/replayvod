import { createFileRoute, Outlet } from "@tanstack/react-router";
import { useStore } from "@tanstack/react-store";
import { useTranslation } from "react-i18next";
import { Navbar } from "@/components/layout/navbar";
import {
	SIDEBAR_EASE,
	SIDEBAR_MARGIN_COLLAPSED,
	SIDEBAR_MARGIN_EXPANDED,
	Sidebar,
} from "@/components/layout/sidebar";
import { useRequireAuth } from "@/hooks/useRequireAuth";
import { cn } from "@/lib/utils";
import { uiStore } from "@/stores/ui";

export const Route = createFileRoute("/dashboard")({
	component: DashboardLayout,
});

function DashboardLayout() {
	const { t } = useTranslation();
	const { isLoading, isAuthenticated } = useRequireAuth();
	const collapsed = useStore(uiStore, (s) => s.sidebarCollapsed);

	if (isLoading) {
		return (
			<div className="flex min-h-screen items-center justify-center">
				<div className="text-muted-foreground">{t("common.loading")}</div>
			</div>
		);
	}

	if (!isAuthenticated) {
		return null;
	}

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
