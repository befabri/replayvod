import { Outlet, createFileRoute } from "@tanstack/react-router"
import { useRequireAuth } from "@/hooks/useRequireAuth"

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
		return null // useRequireAuth triggers navigate to /login
	}

	return <Outlet />
}
