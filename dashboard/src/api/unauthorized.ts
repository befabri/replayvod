let onUnauthorized: (() => void) | null = null;

let redirecting = false;

export function registerUnauthorizedRedirect(handler: () => void) {
	onUnauthorized = handler;
	redirecting = false;
}

export function redirectToLogin() {
	if (redirecting || !onUnauthorized) return;
	redirecting = true;
	onUnauthorized();
}

export function isUnauthorized(error: unknown): boolean {
	const data = (
		error as { data?: { httpStatus?: number; code?: string } } | null
	)?.data;
	return data?.httpStatus === 401 || data?.code === "UNAUTHORIZED";
}

export function handleApiError(error: unknown) {
	if (isUnauthorized(error)) redirectToLogin();
}
