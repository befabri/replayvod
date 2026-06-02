import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { hasRole, resolveSession } from "@/stores/auth";

export const Route = createFileRoute("/dashboard/system")({
	// The parent /dashboard guard already resolved the session; this adds the
	// owner-only check in the loader phase so a non-owner is redirected before the
	// system chrome renders.
	beforeLoad: async () => {
		const user = await resolveSession();
		if (!user) throw redirect({ to: "/login", search: { error: undefined } });
		if (!hasRole(user, "owner")) throw redirect({ to: "/dashboard" });
	},
	component: SystemLayout,
});

function SystemLayout() {
	return <Outlet />;
}
