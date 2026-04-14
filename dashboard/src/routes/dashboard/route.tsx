import { createFileRoute, Outlet } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Navbar } from "@/components/layout/navbar";
import { Sidebar } from "@/components/layout/sidebar";
import { useRequireAuth } from "@/hooks/useRequireAuth";

export const Route = createFileRoute("/dashboard")({
	component: DashboardLayout,
});

function DashboardLayout() {
	const { t } = useTranslation();
	const { isLoading, isAuthenticated } = useRequireAuth();

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
			<main className="md:ml-56 mt-16 p-4 md:p-7 mb-4">
				<Outlet />
			</main>
		</div>
	);
}
