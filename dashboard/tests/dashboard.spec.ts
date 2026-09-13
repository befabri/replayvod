import { expect, test } from "@playwright/test";
import {
	mockTrpc,
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
