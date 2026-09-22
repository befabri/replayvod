import { Store } from "@tanstack/store";
import { RoleEnum } from "@/api/generated/enums";
import type { Role } from "@/api/generated/trpc";
import { handleApiError, isUnauthorized } from "@/api/unauthorized";
import { trpcClient } from "@/integrations/tanstack-query/root-provider";

export type { Role };

export const ROLES: readonly Role[] = Object.values(RoleEnum);

export function isRole(value: string): value is Role {
	return ROLES.some((role) => role === value);
}

export interface AuthUser {
	id: string;
	login: string;
	displayName: string;
	email?: string;
	profileImageUrl?: string;
	role: Role;
}

interface AuthState {
	isAuthenticated: boolean;
	user: AuthUser | null;
	isLoading: boolean;
}

function getInitialState(): AuthState {
	return {
		isAuthenticated: false,
		user: null,
		isLoading: true,
	};
}

export const authStore = new Store<AuthState>(getInitialState());

let sessionResolved = false;
let resolvedUser: AuthUser | null = null;

export function setUser(user: AuthUser) {
	resolvedUser = user;
	sessionResolved = true;
	authStore.setState((s) => ({
		...s,
		isAuthenticated: true,
		user,
		isLoading: false,
	}));
}

export function clearUser() {
	resolvedUser = null;
	sessionResolved = true;
	authStore.setState(() => ({
		isAuthenticated: false,
		user: null,
		isLoading: false,
	}));
}

export function resetSessionCache() {
	sessionResolved = false;
	resolvedUser = null;
}

export async function logout(): Promise<void> {
	try {
		await trpcClient.auth.logout.mutate();
	} catch {}
	clearUser();
}

export function setLoading(isLoading: boolean) {
	authStore.setState((s) => ({ ...s, isLoading }));
}

const roleLevel: Record<Role, number> = {
	viewer: 1,
	admin: 2,
	owner: 3,
};

export function hasRole(user: AuthUser | null, required: Role): boolean {
	if (!user) return false;
	return roleLevel[user.role] >= roleLevel[required];
}

function sessionToUser(
	data: Awaited<ReturnType<typeof trpcClient.auth.session.query>>,
): AuthUser {
	if (!isRole(data.role)) {
		throw new Error("Invalid session response: unknown role");
	}
	return {
		id: data.user_id,
		login: data.login,
		displayName: data.display_name,
		email: data.email ?? undefined,
		profileImageUrl: data.profile_image_url ?? undefined,
		role: data.role,
	};
}

let sessionPromise: Promise<AuthUser | null> | null = null;

export function resolveSession(): Promise<AuthUser | null> {
	if (sessionResolved) {
		return Promise.resolve(resolvedUser);
	}
	if (!sessionPromise) {
		sessionPromise = Promise.resolve()
			.then(() => trpcClient.auth.session.query())
			.then(sessionToUser)
			.catch((error: unknown) => {
				if (!isUnauthorized(error)) throw error;
				return null;
			})
			.then((user) => {
				resolvedUser = user;
				sessionResolved = true;
				return user;
			})
			.finally(() => {
				sessionPromise = null;
			});
	}
	return sessionPromise;
}

export const PROBE_COOLDOWN_MS = 10_000;

let revalidatePromise: Promise<void> | null = null;
let lastValidatedAt = 0;

export function revalidateSession(): Promise<void> {
	if (revalidatePromise) return revalidatePromise;
	if (Date.now() - lastValidatedAt < PROBE_COOLDOWN_MS) {
		return Promise.resolve();
	}
	revalidatePromise = (async () => {
		try {
			const user = sessionToUser(await trpcClient.auth.session.query());
			setUser(user);
			lastValidatedAt = Date.now();
		} catch (err) {
			handleApiError(err);
		} finally {
			revalidatePromise = null;
		}
	})();
	return revalidatePromise;
}

export function withSessionProbe(
	handler?: (err: unknown) => void,
): (err: unknown) => void {
	return (err) => {
		void revalidateSession();
		handler?.(err);
	};
}
