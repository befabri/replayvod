import { createFileRoute, Outlet } from "@tanstack/react-router";
import { useRequireAuth } from "@/hooks/useRequireAuth";

export const Route = createFileRoute("/dashboard/system")({
	component: SystemLayout,
});

function SystemLayout() {
	const { isLoading, isAuthenticated, user } = useRequireAuth({
		requiredRole: "owner",
	});

	if (isLoading) {
		return <div className="text-muted-foreground">Loading…</div>;
	}

	// useRequireAuth already navigates away if unauthenticated or under-privileged.
	// Render nothing until the redirect completes.
	if (!isAuthenticated || !user || user.role !== "owner") {
		return null;
	}

	return <Outlet />;
}
