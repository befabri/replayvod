import { Store } from "@tanstack/store";
import { handleApiError, redirectToLogin } from "@/api/unauthorized";
import { trpcClient } from "@/integrations/tanstack-query/root-provider";

export type Role = "viewer" | "admin" | "owner";

const ROLES: readonly Role[] = ["viewer", "admin", "owner"];

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

// resolvedUser/sessionResolved cache the session result for beforeLoad guards
// without touching authStore. Mutating a subscribed store from a guard (which
// runs mid-navigation, before the protected layout mounts) produced
// update-before-mount and hydration warnings, so the store is hydrated from this
// cache only after the layout mounts. setUser/clearUser keep the cache in
// lockstep with the store so guards and components never disagree.
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

// resetSessionCache forces the next resolveSession() to refetch. A running app
// only re-resolves via a full-page reload (which resets this module), so this
// exists for tests and any future forced re-resolution.
export function resetSessionCache() {
	sessionResolved = false;
	resolvedUser = null;
}

// logout calls the server to delete the session, then clears local state.
// If the server call fails, local state is still cleared so the UI
// reflects logged-out.
export async function logout(): Promise<void> {
	try {
		await trpcClient.auth.logout.mutate();
	} catch {
		// Ignore — we still clear local state below.
	}
	clearUser();
}

export function setLoading(isLoading: boolean) {
	authStore.setState((s) => ({ ...s, isLoading }));
}

// roleLevel maps roles to their hierarchy level. Higher = more permissions.
const roleLevel: Record<Role, number> = {
	viewer: 1,
	admin: 2,
	owner: 3,
};

export function hasRole(user: AuthUser | null, required: Role): boolean {
	if (!user) return false;
	return roleLevel[user.role] >= roleLevel[required];
}

// sessionToUser maps the auth.session response to an AuthUser, or null when the
// server returns a role outside the known set (treated as no valid session).
function sessionToUser(
	data: Awaited<ReturnType<typeof trpcClient.auth.session.query>>,
): AuthUser | null {
	if (!isRole(data.role)) return null;
	return {
		id: data.user_id,
		login: data.login,
		displayName: data.display_name,
		email: data.email ?? undefined,
		profileImageUrl: data.profile_image_url ?? undefined,
		role: data.role,
	};
}

// In-flight session fetch, shared so concurrent guard calls (nested routes
// resolving together on a cold load) hit the endpoint once.
let sessionPromise: Promise<AuthUser | null> | null = null;

// resolveSession resolves the session for route beforeLoad guards. It is pure
// with respect to authStore: guards call it to decide redirects, and the
// protected layout hydrates the store from the result after it mounts (see
// dashboard/route.tsx). The result is cached after the first fetch, so nested
// guards on a cold load and later re-navigations resolve without refetching.
//
// After clearUser() (logout) it returns null without refetching; a fresh login
// is a full-page OAuth round-trip that reboots this module.
export function resolveSession(): Promise<AuthUser | null> {
	if (sessionResolved) {
		return Promise.resolve(resolvedUser);
	}
	if (!sessionPromise) {
		sessionPromise = (async () => {
			try {
				resolvedUser = sessionToUser(await trpcClient.auth.session.query());
			} catch {
				resolvedUser = null;
			} finally {
				sessionResolved = true;
				sessionPromise = null;
			}
			return resolvedUser;
		})();
	}
	return sessionPromise;
}

// After a probe confirms a live session, skip re-probing for this long so a
// flapping connection (repeated SSE drops) can't hammer the session endpoint.
// A genuine expiry inside the window is still caught by the next query or
// navigation, or by the next probe once the window passes.
export const PROBE_COOLDOWN_MS = 10_000;

// revalidateSession re-checks the session after a live (SSE) stream drops.
// EventSource hides the HTTP status, so a stream that failed because the session
// expired is indistinguishable from a transient network blip. Probing the
// session endpoint (whose status we can read) disambiguates: a 401, or a 200
// with no usable session, redirects to login; a success means it was a blip and
// the stream is left to reconnect. Concurrent drops share one in-flight probe.
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
			if (user) {
				setUser(user);
				lastValidatedAt = Date.now();
			} else {
				// 200 with an unrecognized role: the session is unusable, so treat it
				// as gone and redirect rather than silently clearing local state and
				// leaving the user on a dashboard that can't load.
				redirectToLogin();
			}
		} catch (err) {
			// A 401 hands off to the shared interceptor, which clears auth and
			// redirects exactly once. Any other error (network blip, server bounce)
			// is ignored so the subscription can reconnect on its own.
			handleApiError(err);
		} finally {
			revalidatePromise = null;
		}
	})();
	return revalidatePromise;
}

// withSessionProbe wraps a subscription onError so a dropped stream also
// re-checks the session. Pass the subscription's existing onError to keep its
// reconnect bookkeeping; the probe runs first.
export function withSessionProbe(
	handler?: (err: unknown) => void,
): (err: unknown) => void {
	return (err) => {
		void revalidateSession();
		handler?.(err);
	};
}
