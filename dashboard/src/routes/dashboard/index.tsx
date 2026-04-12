import { createFileRoute } from "@tanstack/react-router"
import { useStore } from "@tanstack/react-store"
import { authStore } from "@/stores/auth"

export const Route = createFileRoute("/dashboard/")({
	component: DashboardHome,
})

function DashboardHome() {
	const user = useStore(authStore, (s) => s.user)

	return (
		<div className="p-8">
			<h1 className="text-3xl font-bold mb-4">Dashboard</h1>
			<p className="text-muted-foreground">
				Welcome{user ? `, ${user.displayName}` : ""} ({user?.role})
			</p>
		</div>
	)
}
