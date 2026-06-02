// The query + mutation caches are built with the QueryClient in getContext(),
// before the router exists, so they can only reach the router through this
// late-bound handler. getRouter() registers it once the router is created.
let onUnauthorized: (() => void) | null = null;

// One-shot guard: when a session expires, every in-flight query/mutation fails
// with 401 at once. Without this, each failure would clear auth and navigate
// again. A fresh login is a full-page OAuth round-trip that reboots the app and
// resets this module, so it never needs re-arming within a session.
let redirecting = false;

export function registerUnauthorizedRedirect(handler: () => void) {
	onUnauthorized = handler;
	// Re-arm on (re)registration so a fresh app/router boot starts clean.
	redirecting = false;
}

// redirectToLogin fires the registered redirect at most once per session. Every
// way the app discovers a lost session funnels through here (a 401 from the API
// via handleApiError, or a probe that finds no usable session via
// revalidateSession), so the one-shot guard and the single redirect path are
// shared regardless of how the loss was detected.
export function redirectToLogin() {
	if (redirecting || !onUnauthorized) return;
	redirecting = true;
	onUnauthorized();
}

export function isUnauthorized(error: unknown): boolean {
	// tRPC surfaces the server's httpStatus/code on error.data (see $ErrorShape
	// in the generated client): a 401 is the auth middleware rejecting an
	// expired or missing session cookie. A structural read (rather than an
	// instanceof check) stays robust across bundle boundaries, where a tRPC
	// error can fail instanceof against a differently-loaded class.
	const data = (
		error as { data?: { httpStatus?: number; code?: string } } | null
	)?.data;
	return data?.httpStatus === 401 || data?.code === "UNAUTHORIZED";
}

// handleApiError is the QueryCache/MutationCache onError hook. A 401 from any
// query or mutation means the session is gone, so it drives the shared redirect.
// beforeLoad guards only run on navigation, so a session that expires mid-session
// (no navigation) would otherwise leave the user on a dashboard that silently
// fails every request.
export function handleApiError(error: unknown) {
	if (isUnauthorized(error)) redirectToLogin();
}
