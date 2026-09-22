import { expect, test } from "@playwright/test";
import {
	mockSubscription,
	mockTrpc,
	procsOf,
	SESSION,
	trpcOk,
	trpcUnauthorized,
	validSession,
} from "./support/trpc";

// These specs mock the same-origin /trpc endpoint, so no Go backend is needed.
// Cover session recovery, genuine unauthorized redirects, explicit sign-out,
// and a session that expires mid-use against the production bundle.
test.describe("auth", () => {
	for (const failure of [
		"server error",
		"network failure",
		"unknown role",
	] as const) {
		test(`Retry recovers the original protected page after ${failure}`, async ({
			page,
		}) => {
			const requestedPath =
				"/dashboard/videos?tab=continue_watching&view=grid&sort=recently_watched#resume";
			const navigations: string[] = [];
			const oauthRequests: string[] = [];
			const protectedRequests: string[] = [];
			let documents = 0;
			let sessionRequests = 0;
			let unavailable = true;
			page.on("framenavigated", (frame) => {
				if (frame === page.mainFrame())
					navigations.push(new URL(frame.url()).pathname);
			});
			page.on("request", (request) => {
				const url = new URL(request.url());
				if (
					url.pathname.startsWith("/api/v1/auth/") ||
					url.hostname === "id.twitch.tv"
				) {
					oauthRequests.push(request.url());
				}
				if (
					request.isNavigationRequest() &&
					request.frame() === page.mainFrame()
				)
					documents++;
			});
			await mockSubscription(page, "storage.statusLive");
			await mockTrpc(page, validSession);
			await page.route("**/trpc/**", async (route) => {
				const procs = procsOf(route.request().url());
				if (!procs.includes("auth.session")) {
					protectedRequests.push(...procs);
					await route.fallback();
					return;
				}
				sessionRequests++;
				if (!unavailable) {
					await route.fallback();
					return;
				}
				if (failure === "network failure") {
					await route.abort("failed");
					return;
				}
				await route.fulfill({
					status: failure === "server error" ? 500 : 200,
					contentType: "application/json",
					body: JSON.stringify(
						failure === "unknown role"
							? trpcOk([{ ...SESSION, role: "superadmin" }])
							: [
									{
										error: {
											message: "Database unavailable",
											code: -32603,
											data: { code: "INTERNAL_SERVER_ERROR", httpStatus: 500 },
										},
									},
								],
					),
				});
			});

			await page.goto(requestedPath);
			const error = page.getByRole("alert");
			const retry = error.getByRole("button", { name: "Retry", exact: true });
			await expect(
				error.getByRole("heading", { name: "Could not load this page" }),
			).toBeVisible();
			await expect(page).toHaveURL(requestedPath);
			await expect(
				page.getByRole("button", { name: "Open user menu" }),
			).toHaveCount(0);
			expect(protectedRequests).toEqual([]);
			expect(sessionRequests).toBe(1);

			// A failed retry stays recoverable, with no auth cache reset or page reload.
			await retry.click();
			await expect.poll(() => sessionRequests).toBe(2);
			await expect(retry).toBeEnabled();
			await expect(page).toHaveURL(requestedPath);
			expect(protectedRequests).toEqual([]);

			unavailable = false;
			await retry.click();
			await expect(
				page.getByRole("button", { name: "Open user menu" }),
			).toBeVisible();
			await expect(
				page.getByRole("tab", { name: "Continue watching", exact: true }),
			).toHaveAttribute("aria-selected", "true");
			await expect(page).toHaveURL(requestedPath);
			expect(sessionRequests).toBe(3);
			expect(navigations).not.toContain("/login");
			expect(oauthRequests).toEqual([]);
			expect(documents).toBe(1);
		});
	}

	test("Retry recovers the index session guard", async ({ page }) => {
		let unavailable = true;
		await mockSubscription(page, "storage.statusLive");
		await mockTrpc(page, (procs, url) => {
			if (unavailable && procs.includes("auth.session")) {
				return {
					status: 500,
					body: [
						{
							error: {
								message: "Unavailable",
								code: -32603,
								data: { code: "INTERNAL_SERVER_ERROR", httpStatus: 500 },
							},
						},
					],
				};
			}
			return validSession(procs, url);
		});
		await page.goto("/");
		const retry = page.getByRole("button", { name: "Retry", exact: true });
		await expect(retry).toBeVisible();
		await expect(page).toHaveURL("/");
		unavailable = false;
		await retry.click();
		await expect(page).toHaveURL("/dashboard");
		await expect(
			page.getByRole("button", { name: "Open user menu" }),
		).toBeVisible();
	});

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
		// The route guard reuses the session, so an uncached task request must
		// detect expiration; settings are already cached by the dashboard shell.
		let expired = false;
		await mockTrpc(page, (procs) => {
			if (expired) return { status: 401, body: trpcUnauthorized(procs) };
			return procs.includes("auth.session") ? validSession(procs, "") : null;
		});
		await page.goto("/dashboard");
		await page.getByRole("button", { name: "System", exact: true }).click();
		const tasks = page.getByRole("link", { name: "Tasks", exact: true });
		await expect(tasks).toBeVisible();
		const rejectedRequest = page.waitForResponse(
			(response) =>
				response.status() === 401 &&
				procsOf(response.url()).includes("task.list"),
		);
		expired = true;
		await tasks.click();
		await rejectedRequest;
		await expect(page).toHaveURL(/\/login$/);
	});
});
