import { expect, test } from "@playwright/test";
import {
	mockTrpc,
	trpcUnauthorized,
	validSession,
} from "./support/trpc";

// These specs mock the same-origin /trpc endpoint, so no Go backend is needed.
// They cover the three unauthenticated paths into the app (cold guard, deep
// route), explicit sign-out, and a session that expires mid-use.
test.describe("auth", () => {
	test("login page shows the Twitch connect action", async ({ page }) => {
		await page.goto("/login");
		await expect(page.locator('a[href*="auth/twitch"]')).toBeVisible();
	});

	test("unauthenticated visit to /dashboard redirects to /login", async ({
		page,
	}) => {
		await mockTrpc(page, (procs) =>
			procs.includes("auth.session")
				? { status: 401, body: trpcUnauthorized(procs) }
				: null,
		);
		await page.goto("/dashboard");
		await expect(page).toHaveURL(/\/login$/);
		await expect(page.locator('a[href*="auth/twitch"]')).toBeVisible();
	});

	test("unauthenticated visit to a deep protected route redirects to /login", async ({
		page,
	}) => {
		await mockTrpc(page, (procs) =>
			procs.includes("auth.session")
				? { status: 401, body: trpcUnauthorized(procs) }
				: null,
		);
		await page.goto("/dashboard/system/users");
		await expect(page).toHaveURL(/\/login$/);
	});

	test("signing out clears the session and redirects to /login", async ({
		page,
	}) => {
		await mockTrpc(page, validSession);
		await page.goto("/dashboard");
		// The profile menu lives in the dashboard layout, above the routed
		// Outlet, so it renders as soon as the session resolves regardless of
		// what the index queries return. Clicking it asserts we reached the
		// authenticated shell, independent of any page content.
		await page.getByRole("button", { name: "Open user menu" }).click();
		await page.getByRole("menuitem", { name: "Sign out" }).click();
		await expect(page).toHaveURL(/\/login$/);
	});

	test("a 401 mid-session (expired cookie) redirects to /login", async ({
		page,
	}) => {
		// Load authenticated, then let the cookie expire server-side: every later
		// request 401s, auth.session included. The route guard still passes on
		// the cached session (ensureSession does not refetch once loaded), so the
		// redirect is driven by the cache interceptor (or the SSE probe) when the
		// next client-side navigation fetches data, with no manual sign-out.
		let expired = false;
		await mockTrpc(page, (procs) => {
			if (expired) return { status: 401, body: trpcUnauthorized(procs) };
			return procs.includes("auth.session") ? validSession(procs, "") : null;
		});
		await page.goto("/dashboard");
		await page.getByRole("button", { name: "Open user menu" }).click();
		expired = true;
		await page.getByRole("menuitem", { name: "Settings" }).click();
		await expect(page).toHaveURL(/\/login$/);
	});
});
