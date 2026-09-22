import { createFileRoute, redirect } from "@tanstack/react-router";
import { RouteError } from "@/components/route-error";
import { resolveSession } from "@/stores/auth";

export const Route = createFileRoute("/")({
	errorComponent: RouteError,
	beforeLoad: async () => {
		const user = await resolveSession();
		if (user) throw redirect({ to: "/dashboard" });
		throw redirect({ to: "/login", search: { error: undefined } });
	},
});
