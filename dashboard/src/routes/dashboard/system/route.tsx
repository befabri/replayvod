import { createFileRoute, Outlet } from "@tanstack/react-router";
import { requireRole } from "@/lib/route-guards";

export const Route = createFileRoute("/dashboard/system")({
	beforeLoad: requireRole("admin"),
	component: SystemLayout,
});

function SystemLayout() {
	return <Outlet />;
}
