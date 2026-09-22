import { redirect } from "@tanstack/react-router";
import { hasRole, type Role, resolveSession } from "@/stores/auth";

export function requireRole(role: Role) {
	return async () => {
		const user = await resolveSession();
		if (!user) throw redirect({ to: "/login", search: { error: undefined } });
		if (!hasRole(user, role)) throw redirect({ to: "/dashboard" });
	};
}
