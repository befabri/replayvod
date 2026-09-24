import { expect, type Page, test } from "@playwright/test";
import { USER_SETTINGS } from "../src/test/playback-settings";
import { mockTrpc, SESSION, trpcOk } from "./support/trpc";

// Role-based access specs: nav visibility, route guard redirects, and
// per-feature gating (role dropdown, invites section, request review
// queue) for each of the three roles. All /trpc traffic is mocked, so
// these pin the frontend's own enforcement, not the server's.

type Role = "viewer" | "admin" | "owner";

const NOW = "2026-06-01T12:00:00Z";
const OWNER_PAGES = ["eventsub", "webhook", "playback", "twitch", "tasks", "logs"];

const USERS = [
	{
		id: "u-self",
		login: "self",
		display_name: "Self",
		role: "viewer", // overwritten per spec below
		created_at: NOW,
		updated_at: NOW,
	},
	{
		id: "u-viewer",
		login: "vera",
		display_name: "Vera Viewer",
		role: "viewer",
		created_at: NOW,
		updated_at: NOW,
	},
	{
		id: "u-owner",
		login: "oscar",
		display_name: "Oscar Owner",
		role: "owner",
		created_at: NOW,
		updated_at: NOW,
	},
];

const REQUEST_ROW = {
	id: 1,
	broadcaster_id: "123456",
	broadcaster_login: "chanone",
	broadcaster_name: "Chan One",
	requested_by: "u-viewer",
	requested_by_name: "Vera Viewer",
	note: "please record",
	status: "PENDING",
	created_at: NOW,
};

function dataFor(role: Role, proc: string): unknown {
	switch (proc) {
		case "auth.session":
			return { ...SESSION, user_id: "u-self", role };
		case "settings.get":
			return { ...USER_SETTINGS, user_id: "u-self" };
		case "video.listPage":
			return { items: [] };
		case "system.listUsers":
			return USERS.map((u) => (u.id === "u-self" ? { ...u, role } : u));
		case "system.listInvites":
		case "system.listWhitelist":
			return [];
		case "schedule.myRequests":
		case "schedule.requests":
			return { items: [REQUEST_ROW] };
		case "schedule.list":
			return { data: [] };
		case "schedule.pauseState":
			return { paused: false };
		case "system.fetchLogs":
		case "system.eventLogs":
			return { total: 0, data: [] };
		default:
			return null;
	}
}

async function loginAs(page: Page, role: Role, seen = new Set<string>()) {
	await mockTrpc(page, (procs) => {
		for (const proc of procs) seen.add(proc);
		return { status: 200, body: trpcOk(procs.map((p) => dataFor(role, p))) };
	});
}

test.describe("viewer permissions", () => {
	test("sees neither the Security nor the System nav group", async ({
		page,
	}) => {
		await loginAs(page, "viewer");
		await page.goto("/dashboard");
		await expect(
			page.getByRole("button", { name: "Library", exact: true }),
		).toBeVisible();
		await expect(
			page.getByRole("button", { name: "Security", exact: true }),
		).toHaveCount(0);
		await expect(
			page.getByRole("button", { name: "System", exact: true }),
		).toHaveCount(0);
	});

	test("is redirected away from admin and owner pages", async ({ page }) => {
		await loginAs(page, "viewer");
		for (const path of ["users", "whitelist", ...OWNER_PAGES]) {
			await page.goto(`/dashboard/system/${path}`);
			await expect(page).toHaveURL(/\/dashboard$/);
		}
	});

	test("schedules page offers the request modal and own requests", async ({
		page,
	}) => {
		const seen = new Set<string>();
		await loginAs(page, "viewer", seen);
		await page.goto("/dashboard/schedules");
		// Empty list → the EmptyState carries the request CTA.
		await page.getByRole("button", { name: "Request a channel" }).click();
		await expect(page.getByPlaceholder("Search for a channel…")).toBeVisible();
		await page.keyboard.press("Escape");
		// Own filed requests stay visible (and cancellable) below the list.
		await expect(page.getByRole("cell", { name: /Chan One/ })).toBeVisible();
		await expect(page.getByRole("button", { name: "Cancel" })).toBeVisible();
		await expect(page.getByRole("button", { name: "Approve" })).toHaveCount(0);
		await expect(
			page.getByRole("columnheader", { name: "Requested by" }),
		).toHaveCount(0);
		expect(seen.has("schedule.myRequests")).toBe(true);
		expect(seen.has("schedule.requests")).toBe(false);
	});
});

test.describe("admin permissions", () => {
	test("sees Security with Users + Whitelist but no System group", async ({
		page,
	}) => {
		await loginAs(page, "admin");
		await page.goto("/dashboard");
		const security = page.getByRole("button", {
			name: "Security",
			exact: true,
		});
		// Wait for the authenticated shell to finish its initial route load.
		await expect(security).toBeVisible({ timeout: 30_000 });
		await security.click();
		await expect(
			page.getByRole("link", { name: "Users", exact: true }),
		).toBeVisible();
		await expect(
			page.getByRole("link", { name: "Whitelist", exact: true }),
		).toBeVisible();
		await expect(
			page.getByRole("button", { name: "System", exact: true }),
		).toHaveCount(0);
		await expect(
			page.locator('a[href="/dashboard/system/eventsub"]'),
		).toHaveCount(0);
	});

	test("reaches the users page with the invites section; owner pages stay closed", async ({
		page,
	}) => {
		await loginAs(page, "admin");
		await page.goto("/dashboard/system/users");
		await expect(page).toHaveURL(/\/dashboard\/system\/users$/);
		await expect(
			page.getByRole("heading", { name: "Users", exact: true }),
		).toBeVisible();
		await expect(
			page.getByRole("heading", { name: "Invites", exact: true }),
		).toBeVisible();
		for (const path of OWNER_PAGES) {
			await page.goto(`/dashboard/system/${path}`);
			await expect(page).toHaveURL(/\/dashboard$/);
		}
	});

	test("cannot grant owner: no Owner option, owner rows locked", async ({
		page,
	}) => {
		await loginAs(page, "admin");
		await page.goto("/dashboard/system/users");
		const viewerSelect = page
			.getByRole("row", { name: /Vera Viewer/ })
			.getByRole("combobox", { name: "Role" });
		await expect(viewerSelect).toBeEnabled();
		await viewerSelect.click();
		await expect(
			page.getByRole("listbox", { name: "Role" }).getByRole("option"),
		).toHaveText(["Viewer", "Admin"]);
		await page.keyboard.press("Escape");
		const ownerSelect = page
			.getByRole("row", { name: /Oscar Owner/ })
			.getByRole("combobox", { name: "Role" });
		await expect(ownerSelect).toBeDisabled();
	});

	test("schedules page embeds the review queue with approve and reject", async ({
		page,
	}) => {
		const seen = new Set<string>();
		await loginAs(page, "admin", seen);
		await page.goto("/dashboard/schedules");
		await expect(
			page.getByRole("columnheader", { name: "Requested by" }),
		).toBeVisible();
		await expect(page.getByRole("button", { name: "Approve" })).toBeVisible();
		await expect(page.getByRole("button", { name: "Reject" })).toBeVisible();
		expect(seen.has("schedule.requests")).toBe(true);
	});

	test("shows a failed review queue load", async ({ page }) => {
		await mockTrpc(page, (procs) => ({
			status: 200,
			body: procs.map((proc) =>
				proc === "schedule.requests"
					? {
							error: {
								message: "queue unavailable",
								code: -32603,
								data: { code: "INTERNAL_SERVER_ERROR", httpStatus: 500 },
							},
						}
					: { result: { data: dataFor("admin", proc) } },
			),
		}));
		await page.goto("/dashboard/schedules");
		await expect(page.getByText(/queue unavailable/)).toBeVisible({
			timeout: 15_000,
		});
	});
});

for (const role of ["viewer", "admin", "owner"] as const) {
	test(`${role} cannot act on decided requests`, async ({ page }) => {
		await mockTrpc(page, (procs) => ({
			status: 200,
			body: trpcOk(
				procs.map((proc) =>
					proc === "schedule.myRequests" || proc === "schedule.requests"
						? {
								items: [
									REQUEST_ROW,
									{
										...REQUEST_ROW,
										id: 2,
										broadcaster_name: "Approved channel",
										status: "APPROVED",
									},
									{
										...REQUEST_ROW,
										id: 3,
										broadcaster_name: "Rejected channel",
										status: "REJECTED",
									},
								],
							}
						: dataFor(role, proc),
				),
			),
		}));
		await page.goto("/dashboard/schedules");
		for (const name of ["Approved channel", "Rejected channel"]) {
			const row = page.getByRole("row", { name: new RegExp(name) });
			await expect(row).toBeVisible();
			await expect(row.getByRole("button")).toHaveCount(0);
		}
		const pending = page.getByRole("row", { name: /Chan One/ });
		await expect(
			pending.getByRole("button", {
				name: role === "viewer" ? "Cancel" : "Approve",
			}),
		).toBeVisible();
	});
}

test("only unredeemed invites can be revoked", async ({ page }) => {
	const base = {
		id: 1,
		role: "viewer",
		created_by: "u-self",
		created_at: NOW,
		expires_at: "2099-01-01T00:00:00Z",
		note: "Pending link",
	};
	await mockTrpc(page, (procs) => ({
		status: 200,
		body: trpcOk(
			procs.map((proc) =>
				proc === "system.listInvites"
					? [
							base,
							{
								...base,
								id: 2,
								note: "Expired link",
								expires_at: "2000-01-01T00:00:00Z",
							},
							{
								...base,
								id: 3,
								note: "Redeemed link",
								redeemed_at: NOW,
								redeemed_by: "u-viewer",
							},
						]
					: dataFor("admin", proc),
			),
		),
	}));
	await page.goto("/dashboard/system/users");
	await expect(
		page
			.getByRole("row", { name: /Pending link/ })
			.getByRole("button", { name: "Revoke" }),
	).toBeVisible();
	await expect(
		page
			.getByRole("row", { name: /Expired link/ })
			.getByRole("button", { name: "Revoke" }),
	).toBeVisible();
	const redeemed = page.getByRole("row", { name: /Redeemed link/ });
	await expect(redeemed).toBeVisible();
	await expect(redeemed.getByRole("button", { name: "Revoke" })).toHaveCount(0);
});

test("a pending invite can issue a new link that is shown once", async ({
	page,
}) => {
	const base = {
		id: 1,
		role: "viewer",
		created_by: "u-self",
		created_at: NOW,
		expires_at: "2099-01-01T00:00:00Z",
		note: "Pending link",
	};
	const freshUrl = "http://localhost:39173/invite/fresh-token";
	let rotated = 0;
	await mockTrpc(page, (procs) => {
		if (procs.includes("system.rotateInvite")) {
			rotated++;
			return {
				status: 200,
				body: trpcOk([
					{ id: 1, role: "viewer", expires_at: base.expires_at, url: freshUrl },
				]),
			};
		}
		return {
			status: 200,
			body: trpcOk(
				procs.map((proc) =>
					proc === "system.listInvites"
						? [
								base,
								{
									...base,
									id: 2,
									note: "Expired link",
									expires_at: "2000-01-01T00:00:00Z",
								},
								{
									...base,
									id: 3,
									note: "Redeemed link",
									redeemed_at: NOW,
									redeemed_by: "u-viewer",
								},
							]
						: dataFor("admin", proc),
				),
			),
		};
	});
	await page.goto("/dashboard/system/users");
	// The users table shows the Twitch ID so "Redeemed by" can be matched.
	await expect(page.getByRole("row", { name: /Vera Viewer/ })).toContainText(
		"u-viewer",
	);
	for (const name of [/Expired link/, /Redeemed link/]) {
		await expect(
			page.getByRole("row", { name }).getByRole("button", { name: "New link" }),
		).toHaveCount(0);
	}
	await expect(page.getByText(freshUrl)).toHaveCount(0);
	await page
		.getByRole("row", { name: /Pending link/ })
		.getByRole("button", { name: "New link" })
		.click();
	await expect(page.getByText(freshUrl)).toBeVisible();
	expect(rotated).toBe(1);
	// The link lives only in page state: a refresh must not bring it back.
	await page.reload();
	await expect(
		page.getByRole("heading", { name: "Invites", exact: true }),
	).toBeVisible();
	await expect(page.getByText(freshUrl)).toHaveCount(0);
});

test("a rejected new link reports the failure and refreshes the row", async ({
	page,
}) => {
	const base = {
		id: 1,
		role: "viewer",
		created_by: "u-self",
		created_at: NOW,
		expires_at: "2099-01-01T00:00:00Z",
		note: "Pending link",
	};
	let rotateAttempted = false;
	await mockTrpc(page, (procs) => {
		if (procs.includes("system.rotateInvite")) {
			rotateAttempted = true;
			return {
				status: 404,
				body: [
					{
						error: {
							message: "invite not found",
							code: -32004,
							data: { code: "NOT_FOUND", httpStatus: 404 },
						},
					},
				],
			};
		}
		return {
			status: 200,
			body: trpcOk(
				procs.map((proc) => {
					if (proc !== "system.listInvites") return dataFor("admin", proc);
					// The invite expired between the first render and the click;
					// the refetch after the failed rotate reveals it.
					return [
						rotateAttempted
							? { ...base, expires_at: "2000-01-01T00:00:00Z" }
							: base,
					];
				}),
			),
		};
	});
	await page.goto("/dashboard/system/users");
	const row = page.getByRole("row", { name: /Pending link/ });
	await row.getByRole("button", { name: "New link" }).click();
	await expect(page.getByText("Failed to create a new link")).toBeVisible();
	await expect(row).toContainText("Expired");
	await expect(row.getByRole("button", { name: "New link" })).toHaveCount(0);
	await expect(row.getByRole("button", { name: "Revoke" })).toBeVisible();
});

test.describe("owner permissions", () => {
	test("sees the System group and reaches owner-only pages", async ({
		page,
	}) => {
		await loginAs(page, "owner");
		await page.goto("/dashboard");
		await expect(
			page.getByRole("button", { name: "System", exact: true }),
		).toBeVisible();
		await page.goto("/dashboard/system/logs");
		await expect(page).toHaveURL(/\/dashboard\/system\/logs$/);
		await expect(
			page.getByRole("heading", { name: "Logs", exact: true }),
		).toBeVisible();
	});

	test("role dropdown offers Owner and can edit owner rows", async ({
		page,
	}) => {
		await loginAs(page, "owner");
		await page.goto("/dashboard/system/users");
		const viewerSelect = page
			.getByRole("row", { name: /Vera Viewer/ })
			.getByRole("combobox", { name: "Role" });
		await viewerSelect.click();
		await expect(
			page.getByRole("listbox", { name: "Role" }).getByRole("option"),
		).toHaveText(["Viewer", "Admin", "Owner"]);
		await page.keyboard.press("Escape");
		const ownerSelect = page
			.getByRole("row", { name: /Oscar Owner/ })
			.getByRole("combobox", { name: "Role" });
		await expect(ownerSelect).toBeEnabled();
	});
});

test.describe("invite landing", () => {
	test("shows the Twitch continue link carrying the raw token", async ({
		page,
	}) => {
		await mockTrpc(page, () => null);
		await page.goto("/invite/tok-123");
		await expect(
			page.getByRole("heading", { name: "You've been invited" }),
		).toBeVisible();
		await expect(
			page.locator('a[href*="auth/twitch?invite=tok-123"]'),
		).toBeVisible();
	});
});

for (const role of ["viewer", "admin"] as const) {
	test(`${role} can load older request history`, async ({ page }) => {
		const cursor = { id: REQUEST_ROW.id, created_at: NOW };
		let loadedNext = false;
		await mockTrpc(page, (procs, url) => {
			const input = JSON.parse(new URL(url).searchParams.get("input") ?? "{}");
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc, index) => {
						if (proc !== "schedule.myRequests" && proc !== "schedule.requests")
							return dataFor(role, proc);
						if (input[String(index)]?.cursor) {
							expect(input[String(index)].cursor).toEqual(cursor);
							loadedNext = true;
							return {
								items: [
									{
										...REQUEST_ROW,
										id: 99,
										broadcaster_name: "Older request",
										status: "REJECTED",
									},
								],
							};
						}
						return { items: [REQUEST_ROW], next_cursor: cursor };
					}),
				),
			};
		});
		await page.goto("/dashboard/schedules");
		await expect(page.getByText("Chan One", { exact: true })).toBeVisible({
			timeout: 30_000,
		});
		await page.getByRole("button", { name: "Show more", exact: true }).click();
		await expect(
			page.getByText("Older request", { exact: true }),
		).toBeVisible();
		await expect(page.getByText("Chan One", { exact: true })).toBeVisible();
		await expect(
			page.getByRole("button", { name: "Show more", exact: true }),
		).toHaveCount(0);
		expect(loadedNext).toBe(true);
	});
}
