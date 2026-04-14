import { useEffect } from "react";
import { trpcClient } from "@/integrations/tanstack-query/root-provider";
import { clearUser, type Role, setUser } from "@/stores/auth";

// useAuthInit hydrates the auth store from the server session cookie on
// app start. Called once in __root.tsx.
//
// Uses the vanilla trpcClient rather than the TanStack Query `useTRPC`
// hook because the result lands in the Store (not the query cache); no
// background refetch, no invalidation tracking. A one-shot call is the
// simpler shape.
export function useAuthInit() {
	useEffect(() => {
		const controller = new AbortController();
		(async () => {
			try {
				const data = await trpcClient.auth.session.query(undefined, {
					signal: controller.signal,
				});
				setUser({
					id: data.user_id,
					login: data.login,
					displayName: data.display_name,
					email: data.email ?? undefined,
					profileImageUrl: data.profile_image_url ?? undefined,
					role: data.role as Role,
				});
			} catch (err) {
				if ((err as Error).name !== "AbortError") {
					clearUser();
				}
			}
		})();
		return () => controller.abort();
	}, []);
}
