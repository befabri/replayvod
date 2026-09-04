import { createFileRoute, Outlet } from "@tanstack/react-router";
import { requireRole } from "@/lib/route-guards";

export const Route = createFileRoute("/dashboard/system")({
	// The parent /dashboard guard already resolved the session; this adds the
	// admin check in the loader phase so a viewer is redirected before the
	// system chrome renders. The owner-only pages (eventsub, webhook,
	// playback, tasks, logs) layer requireRole("owner") on top per-route.
	beforeLoad: requireRole("admin"),
	component: SystemLayout,
});

function SystemLayout() {
	return <Outlet />;
}
