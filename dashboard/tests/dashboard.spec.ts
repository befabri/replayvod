import { expect, test } from "@playwright/test";
import {
	mockTrpc,
	procsOf,
	SESSION,
	trpcOk,
	validSession,
} from "./support/trpc";
import { userState, videoRecording } from "./support/watch";

// Mocks the same-origin /trpc endpoint, so no Go backend is needed. The
// active-downloads SSE subscription is aborted by the mock (EventSource just
// retries), so the live rows never populate — but the index route still mounts
// the RunningDownloads section, which is what this exercises end to end: the
// authenticated shell + dashboard index render in a real browser.
test.describe("dashboard", () => {
	test("authenticated dashboard mounts the Running Now section", async ({
		page,
	}) => {
		await mockTrpc(page, validSession);
		await page.goto("/dashboard");
		// The profile menu confirms we reached the authenticated shell.
		await expect(
			page.getByRole("button", { name: "Open user menu" }),
		).toBeVisible({ timeout: 30_000 });
		// The heading lives outside the loading/empty/error branch, so it renders
		// regardless of the (aborted) subscription state.
		await expect(
			page.getByRole("heading", { name: "Running now" }),
		).toBeVisible();
		// Nothing started: no continue-watching strip.
		await expect(page.getByTestId("continue-watching")).toHaveCount(0);
		await expect(page.getByTestId("latest-recordings")).toHaveCount(0);
	});

	test("lists the recordings the viewer is partway through", async ({
		page,
	}) => {
		await mockTrpc(page, (procs, url) => {
			const session = validSession(procs, url);
			if (session) return session;
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) =>
						proc === "video.continueWatching"
							? [
									videoRecording(41, 3600, { user_state: userState(900) }),
									videoRecording(42, 3600, { user_state: userState(3590) }),
								]
							: proc === "video.listPage"
								? { items: [] }
								: null,
					),
				),
			};
		});
		await page.goto("/dashboard");
		const strip = page.getByTestId("continue-watching");
		await expect(strip).toBeVisible({ timeout: 30_000 });
		await expect(
			strip.getByRole("heading", { name: "Continue watching" }),
		).toBeVisible();
		// The one played to the end opens at the start, so it is not offered.
		await expect(strip.getByTestId("video-card-progress")).toHaveCount(1);
		await expect(strip.getByTestId("video-card-progress")).toHaveAttribute(
			"aria-valuenow",
			"25",
		);
		await expect(
			strip.getByRole("link", { name: "Watch Resume fixture" }),
		).toHaveAttribute("href", "/dashboard/watch/41");
		const viewAll = strip.getByRole("link", { name: "View all" });
		await expect(viewAll).toHaveAttribute("href", /tab=continue_watching/);
		await viewAll.click();
		await expect(page).toHaveURL(/\/dashboard\/videos\?tab=continue_watching/);
		await expect(
			page.getByRole("tab", { name: "Continue watching" }),
		).toHaveAttribute("aria-selected", "true");
	});
});

test("latest recordings shows five completed videos and View all opens the completed library", async ({
	page,
}) => {
	const inputs: Record<string, unknown>[] = [];
	await mockTrpc(page, (procs, url) => {
		const batch = JSON.parse(new URL(url).searchParams.get("input") ?? "{}");
		return {
			status: 200,
			body: trpcOk(
				procs.map((proc, index) => {
					if (proc === "auth.session") return SESSION;
					if (proc !== "video.listPage") return null;
					const input = batch[index];
					inputs.push(input);
					const recordings = [6, 5, 4, 3, 2, 1].map((id) =>
						videoRecording(id, 3600, { title: `Recording ${id}` }),
					);
					return { items: recordings.slice(0, input.limit) };
				}),
			),
		};
	});
	await page.goto("/dashboard");
	const latest = page.getByTestId("latest-recordings");
	await expect(
		latest.getByRole("heading", { name: "Latest recordings" }),
	).toBeVisible();
	await expect(
		latest.getByRole("link", { name: /^Watch Recording/ }),
	).toHaveCount(5);
	await expect(
		latest.getByRole("link", { name: "Watch Recording 1", exact: true }),
	).toHaveCount(0);
	expect(inputs).toContainEqual(
		expect.objectContaining({
			limit: 5,
			status: "DONE",
			sort: "created_at",
			order: "desc",
		}),
	);
	await latest.getByRole("link", { name: "View all" }).click();
	await expect(page).toHaveURL(/status=DONE/);
	await expect(
		page.getByRole("link", { name: "Watch Recording 1", exact: true }),
	).toBeVisible();
	expect(inputs).toContainEqual(
		expect.objectContaining({
			limit: 50,
			status: "DONE",
			sort: "created_at",
			order: "desc",
		}),
	);
});

test("Continue Watching keeps recently watched order through View all and pagination", async ({
	page,
}) => {
	const recordings = Array.from({ length: 51 }, (_, index) =>
		videoRecording(index + 1, 3600, {
			title: `Recently watched ${index + 1}`,
			start_download_at:
				index === 0 ? "2020-01-01T00:00:00Z" : "2026-09-13T00:00:00Z",
			user_state: userState(900),
		}),
	);
	const inputs: Record<string, unknown>[] = [];
	await mockTrpc(page, (procs, url) => {
		const batch = JSON.parse(new URL(url).searchParams.get("input") ?? "{}");
		return {
			status: 200,
			body: trpcOk(
				procs.map((proc, index) => {
					if (proc === "auth.session") return SESSION;
					if (proc === "video.continueWatching") return recordings.slice(0, 5);
					if (proc !== "video.listPage") return null;
					const input = batch[index];
					if (!input.continue_watching_only) return { items: [] };
					inputs.push(input);
					return input.cursor
						? { items: recordings.slice(50) }
						: {
								items: recordings.slice(0, 50),
								next_cursor: {
									id: 50,
									start_download_at: recordings[49].start_download_at,
									sort_int: "1000",
								},
							};
				}),
			),
		};
	});
	await page.goto("/dashboard");
	const strip = page.getByTestId("continue-watching");
	await expect(
		strip.getByRole("link", { name: /^Watch Recently watched/ }).first(),
	).toHaveAttribute("href", "/dashboard/watch/1");
	await strip.getByRole("link", { name: "View all" }).click();
	await expect(page).toHaveURL(/sort=recently_watched/);
	await expect(
		page.getByRole("link", { name: /^Watch Recently watched/ }).first(),
	).toHaveAttribute("href", "/dashboard/watch/1");
	// The virtual grid must load and expose the next page in the same order.
	await expect
		.poll(async () => {
			await page.evaluate(() =>
				window.scrollTo(0, document.documentElement.scrollHeight),
			);
			return inputs.some((input) => input.cursor);
		})
		.toBe(true);
	await expect(
		page.getByRole("link", { name: "Watch Recently watched 51", exact: true }),
	).toBeVisible();
	expect(
		inputs.every(
			(input) => input.sort === "last_watched" && input.order === "desc",
		),
	).toBe(true);
});

test("Latest Recordings shows loading, reports failure, and recovers on retry", async ({
	page,
}) => {
	await mockTrpc(page, validSession);
	let release: () => void = () => {};
	const gate = new Promise<void>((resolve) => {
		release = resolve;
	});
	let failed = true;
	await page.route("**/trpc/**", async (route) => {
		const procs = procsOf(route.request().url());
		if (!procs.includes("video.listPage")) return route.fallback();
		await gate;
		await route.fulfill({
			status: failed ? 500 : 200,
			contentType: "application/json",
			body: JSON.stringify(
				failed
					? procs.map(() => ({
							error: {
								message: "offline",
								code: -32603,
								data: { code: "INTERNAL_SERVER_ERROR", httpStatus: 500 },
							},
						}))
					: trpcOk(
							procs.map((proc) =>
								proc === "video.listPage"
									? { items: [videoRecording(90, 3600)] }
									: null,
							),
						),
			),
		});
	});
	await page.goto("/dashboard");
	const latest = page.getByTestId("latest-recordings");
	await expect(latest.getByRole("status", { name: "Loading…" })).toBeVisible();
	release();
	await expect(latest.getByRole("alert")).toContainText(
		"Failed to load videos",
		{ timeout: 20_000 },
	);
	failed = false;
	await latest.getByRole("button", { name: "Retry", exact: true }).click();
	await expect(
		latest.getByRole("link", { name: "Watch Resume fixture" }),
	).toBeVisible();
	await expect(latest.getByRole("alert")).toHaveCount(0);
});
