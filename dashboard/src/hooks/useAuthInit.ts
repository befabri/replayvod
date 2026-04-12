import { useEffect } from "react"
import { API_URL } from "@/env"
import { setUser, clearUser, type Role } from "@/stores/auth"

// useAuthInit hydrates the auth store from the server session cookie on app start.
// Called once in __root.tsx.
export function useAuthInit() {
	useEffect(() => {
		const controller = new AbortController()
		;(async () => {
			try {
				const res = await fetch(`${API_URL}/trpc/auth.session`, {
					method: "GET",
					credentials: "include",
					signal: controller.signal,
				})
				if (!res.ok) {
					clearUser()
					return
				}
				const body = await res.json()
				// tRPC envelope: { result: { data: ... } }
				const data = body?.result?.data
				if (!data) {
					clearUser()
					return
				}
				setUser({
					id: data.user_id,
					login: data.login,
					displayName: data.display_name,
					email: data.email ?? undefined,
					profileImageUrl: data.profile_image_url ?? undefined,
					role: data.role as Role,
				})
			} catch (err) {
				if ((err as Error).name !== "AbortError") {
					clearUser()
				}
			}
		})()
		return () => controller.abort()
	}, [])
}
