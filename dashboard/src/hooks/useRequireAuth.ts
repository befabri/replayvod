import { useEffect } from "react"
import { useNavigate } from "@tanstack/react-router"
import { useStore } from "@tanstack/react-store"
import { authStore, hasRole, type Role } from "@/stores/auth"

interface UseRequireAuthOptions {
	/** Minimum role required to access the route. Defaults to viewer (any authenticated user). */
	requiredRole?: Role
}

/**
 * Enforces authentication (and optionally a minimum role) on a route.
 * Redirects to /login if unauthenticated, or to /dashboard if authenticated but lacking role.
 * Returns the current auth state so the component can render loading/content.
 */
export function useRequireAuth(options?: UseRequireAuthOptions) {
	const state = useStore(authStore, (s) => s)
	const navigate = useNavigate()

	useEffect(() => {
		if (state.isLoading) return

		if (!state.isAuthenticated || !state.user) {
			navigate({ to: "/login", search: { error: undefined } })
			return
		}

		if (options?.requiredRole && !hasRole(state.user, options.requiredRole)) {
			navigate({ to: "/dashboard" })
		}
	}, [state, options?.requiredRole, navigate])

	return state
}
