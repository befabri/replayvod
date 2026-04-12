import { Outlet, createFileRoute } from "@tanstack/react-router"
import { useRequireAuth } from "@/hooks/useRequireAuth"
import { Sidebar } from "@/components/layout/sidebar"

export const Route = createFileRoute("/dashboard")({
	component: DashboardLayout,
})

function DashboardLayout() {
	const { isLoading, isAuthenticated } = useRequireAuth()

	if (isLoading) {
		return (
			<div className="flex min-h-screen items-center justify-center">
				<div className="text-muted-foreground">Loading…</div>
			</div>
		)
	}

	if (!isAuthenticated) {
		return null
	}

	return (
		<div className="min-h-screen">
			<Sidebar />
			<main className="md:ml-56 pt-14 md:pt-0">
				<Outlet />
			</main>
		</div>
	)
}
