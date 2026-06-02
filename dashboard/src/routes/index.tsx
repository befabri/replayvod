import { createFileRoute, redirect } from "@tanstack/react-router";
import { resolveSession } from "@/stores/auth";

// The index route is a pure dispatcher: resolve the session before anything
// renders, then redirect to the dashboard or login. Doing it in beforeLoad
// (rather than a mount effect that navigates) means there's no transient
// blank/null frame on "/".
export const Route = createFileRoute("/")({
	beforeLoad: async () => {
		const user = await resolveSession();
		if (user) throw redirect({ to: "/dashboard" });
		throw redirect({ to: "/login", search: { error: undefined } });
	},
});
