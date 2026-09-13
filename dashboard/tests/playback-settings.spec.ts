import { expect, test } from "@playwright/test";
import { USER_SETTINGS } from "../src/test/playback-settings";
import { fulfillRangeFixture, videoFixture } from "./support/audio";
import { mockTrpc, procsOf, SESSION, trpcOk } from "./support/trpc";
import { mockWatchPage, userState, videoRecording } from "./support/watch";

test("saves playback preferences, refreshes Continue Watching, and survives reload", async ({
	page,
}) => {
	let settings = structuredClone(USER_SETTINGS);
	const writes: unknown[] = [];
	const video = videoRecording(41, 100, { user_state: userState(40) });
	await mockTrpc(page, (procs) => ({
		status: 200,
		body: trpcOk(
			procs.map((proc) => {
				if (proc === "auth.session") return SESSION;
				if (proc === "settings.get") return settings;
				if (proc === "video.statistics")
					return {
						total: 1,
						total_size: 0,
						channels: 1,
						continue_watching:
							settings.playback.resume_min_seconds <= 40 ? 1 : 0,
					};
				if (proc === "video.listPage")
					return {
						items: settings.playback.resume_min_seconds <= 40 ? [video] : [],
					};
				return null;
			}),
		),
	}));
	await page.route("**/trpc/settings.updatePlayback*", async (route) => {
		const body = route.request().postDataJSON();
		const input = body[0] ?? body;
		writes.push(input);
		settings = {
			...settings,
			playback: input,
			updated_at: "2026-09-13T12:00:00Z",
		};
		const batch =
			new URL(route.request().url()).searchParams.get("batch") === "1";
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(
				batch ? trpcOk([settings]) : { result: { data: settings } },
			),
		});
	});
	await page.goto("/dashboard/videos?tab=continue_watching");
	await expect(
		page.getByRole("tab", { name: "Continue watching 1" }),
	).toBeVisible();
	await page.getByRole("button", { name: "Open user menu" }).click();
	await page.getByRole("menuitem", { name: "Settings", exact: true }).click();
	const form = page.getByRole("form", { name: "Playback" });
	await page.getByLabel("Time zone", { exact: true }).fill("Europe/Brussels");
	await expect(form.getByLabel("Resume after (seconds)")).toHaveValue("5");
	await form.getByLabel("Resume after (seconds)").fill("50");
	await form.getByLabel("Finish within (seconds)").fill("10");
	await form.getByLabel("Short recording cap (%)").fill("10");
	await form.getByRole("button", { name: "Save", exact: true }).click();
	await expect(form.getByRole("status")).toBeVisible();
	await expect(page.getByLabel("Time zone", { exact: true })).toHaveValue(
		"Europe/Brussels",
	);
	expect(writes).toEqual([
		{
			resume_min_seconds: 50,
			resume_end_margin_seconds: 10,
			resume_end_margin_percent: 10,
		},
	]);
	await page.getByRole("button", { name: "Library", exact: true }).click();
	await page.getByRole("link", { name: "Videos", exact: true }).click();
	await page.getByRole("tab", { name: "Continue watching 0" }).click();
	await expect(
		page.getByText(
			"No videos to continue. Start watching a video to see it here.",
		),
	).toBeVisible();
	await page.goto("/dashboard/settings");
	await expect(form.getByLabel("Resume after (seconds)")).toHaveValue("50");
	await expect(form.getByLabel("Finish within (seconds)")).toHaveValue("10");
});

test("keeps edits on save failure and prevents out-of-range values", async ({
	page,
}) => {
	await mockTrpc(page, (procs) => ({
		status: 200,
		body: trpcOk(
			procs.map((proc) =>
				proc === "auth.session"
					? SESSION
					: proc === "settings.get"
						? USER_SETTINGS
						: null,
			),
		),
	}));
	let calls = 0;
	await page.route("**/trpc/settings.updatePlayback*", async (route) => {
		calls++;
		await route.fulfill({
			status: 500,
			contentType: "application/json",
			body: JSON.stringify([
				{
					error: {
						message: "Save unavailable",
						code: -32603,
						data: { code: "INTERNAL_SERVER_ERROR", httpStatus: 500 },
					},
				},
			]),
		});
	});
	await page.goto("/dashboard/settings");
	const form = page.getByRole("form", { name: "Playback" });
	await form.getByLabel("Resume after (seconds)").fill("0");
	await form.getByRole("button", { name: "Save", exact: true }).click();
	expect(calls).toBe(0);
	await form.getByLabel("Resume after (seconds)").fill("50");
	await form.getByRole("button", { name: "Save", exact: true }).click();
	await expect(form.getByRole("alert")).toBeVisible();
	await expect(form.getByLabel("Resume after (seconds)")).toHaveValue("50");
	expect(calls).toBe(1);
});

test("waits for saved preferences before choosing the initial playback position", async ({
	page,
}) => {
	await mockWatchPage(page, {
		video: () => videoRecording(78, 30, { user_state: userState(12) }),
	});
	await page.route("**/api/v1/videos/78/parts/1/stream", (route) =>
		fulfillRangeFixture(route, videoFixture, "video/mp4"),
	);
	let release: () => void = () => {};
	const gate = new Promise<void>((resolve) => {
		release = resolve;
	});
	let requested = false;
	await page.route("**/trpc/**", async (route) => {
		const procs = procsOf(route.request().url());
		if (!procs.includes("settings.get")) return route.fallback();
		requested = true;
		await gate;
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(
				trpcOk(
					procs.map((proc) =>
						proc === "settings.get"
							? {
									...USER_SETTINGS,
									playback: {
										...USER_SETTINGS.playback,
										resume_min_seconds: 20,
									},
								}
							: null,
					),
				),
			),
		});
	});
	await page.goto("/dashboard/watch/78");
	await expect.poll(() => requested).toBe(true);
	await expect(page.locator("video")).toHaveCount(0);
	release();
	await expect(page.locator("video")).toBeAttached();
	await expect
		.poll(() =>
			page
				.locator("video")
				.evaluate((video: HTMLVideoElement) => video.readyState),
		)
		.toBeGreaterThanOrEqual(1);
	expect(
		await page
			.locator("video")
			.evaluate((video: HTMLVideoElement) => video.currentTime),
	).toBeLessThan(0.5);
	await expect(page.getByTestId("resume-notice")).toHaveCount(0);
});
