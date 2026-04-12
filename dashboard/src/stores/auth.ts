import { Store } from "@tanstack/store"
import { trpcClient } from "@/integrations/tanstack-query/root-provider"

export type Role = "viewer" | "admin" | "owner"

export interface AuthUser {
	id: string
	login: string
	displayName: string
	email?: string
	profileImageUrl?: string
	role: Role
}

interface AuthState {
	isAuthenticated: boolean
	user: AuthUser | null
	isLoading: boolean
}

function getInitialState(): AuthState {
	return {
		isAuthenticated: false,
		user: null,
		isLoading: true,
	}
}

export const authStore = new Store<AuthState>(getInitialState())

export function setUser(user: AuthUser) {
	authStore.setState((s) => ({
		...s,
		isAuthenticated: true,
		user,
		isLoading: false,
	}))
}

export function clearUser() {
	authStore.setState(() => ({
		isAuthenticated: false,
		user: null,
		isLoading: false,
	}))
}

// logout calls the server to delete the session, then clears local state.
// If the server call fails, local state is still cleared so the UI
// reflects logged-out.
export async function logout(): Promise<void> {
	try {
		await trpcClient.auth.logout.mutate()
	} catch {
		// Ignore — we still clear local state below.
	}
	clearUser()
}

export function setLoading(isLoading: boolean) {
	authStore.setState((s) => ({ ...s, isLoading }))
}

// roleLevel maps roles to their hierarchy level. Higher = more permissions.
const roleLevel: Record<Role, number> = {
	viewer: 1,
	admin: 2,
	owner: 3,
}

export function hasRole(user: AuthUser | null, required: Role): boolean {
	if (!user) return false
	return roleLevel[user.role] >= roleLevel[required]
}
