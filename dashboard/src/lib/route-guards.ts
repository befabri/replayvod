import { redirect } from "@tanstack/react-router";
import { hasRole, type Role, resolveSession } from "@/stores/auth";

// requireRole builds a beforeLoad guard that bounces visitors below the
// required role back to the dashboard (and unauthenticated ones to login).
// The /dashboard/system layout requires admin; the owner-only pages under
// it layer requireRole("owner") on top per-route.
export function requireRole(role: Role) {
	return async () => {
		const user = await resolveSession();
		if (!user) throw redirect({ to: "/login", search: { error: undefined } });
		if (!hasRole(user, role)) throw redirect({ to: "/dashboard" });
	};
}
