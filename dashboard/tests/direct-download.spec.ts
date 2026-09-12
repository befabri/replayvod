import { expect, test, type Page } from "@playwright/test";
import type { LiveRenditionsResponse } from "../src/api/generated/trpc";
import { procsOf, SESSION, trpcOk } from "./support/trpc";
import { videoRecording } from "./support/watch";

test.afterEach(async ({ page }) => {
	await page.unrouteAll({ behavior: "ignoreErrors" });
});

async function downloadServer(page: Page, followed = false) {
	const state = {
		live: true,
		connected: false,
		downloads: [] as unknown[],
		liveCalls: 0,
		renditionCalls: 0,
		renditionHeight: undefined as number | undefined,
		emptyRenditions: false,
		beforeLive: async () => {},
		beforeRenditions: async (_h264: boolean) => {},
	};
	const answers: Record<string, unknown> = {
		"auth.session": SESSION,
		"channel.getById": {
			broadcaster_id: "chan1",
			broadcaster_name: "ThornityCo",
			broadcaster_login: "thornityco",
		},
		"stream.liveIds": followed ? ["chan1"] : [],
		"video.byBroadcaster": { items: [] },
		"video.getById": videoRecording(95, 30),
		"video.timeline": [],
		"video.categories": [],
		"video.titles": [],
		"video.statisticsByBroadcaster": { total: 0 },
		"storage.status": { state: "attached" },
	};
	await page.route("**/trpc/**", async (route) => {
		const request = route.request();
		if ((request.headers().accept ?? "").includes("text/event-stream")) {
			await route.abort();
			return;
		}
		const input = JSON.parse(
			request.postData() ??
				new URL(request.url()).searchParams.get("input") ??
				"{}",
		);
		const results = await Promise.all(
			procsOf(request.url()).map(async (proc, index) => {
				if (proc === "stream.isLive") {
					expect(input[index]).toEqual({ broadcaster_id: "chan1" });
					state.liveCalls++;
					await state.beforeLive();
					return state.live;
				}
				if (proc === "video.liveRenditions") {
					expect(input[index].broadcaster_id).toBe("chan1");
					state.renditionCalls++;
					const h264 = input[index].force_h264;
					await state.beforeRenditions(h264);
					return {
						anonymous: !state.connected,
						renditions: state.emptyRenditions ? [] : [
							{
								height: state.renditionHeight ?? (h264 || !state.connected ? 1080 : 1440),
								fps: 60,
								codec: h264 ? "h264" : "h265",
							},
						],
					} satisfies LiveRenditionsResponse;
				}
				if (proc === "video.triggerDownload") {
					state.downloads.push(input[index]);
					return { job_id: "new-job" };
				}
				if (proc === "twitchPlayback.connect") state.connected = true;
				if (proc.startsWith("twitchPlayback."))
					return {
						state: state.connected ? "connected" : "disconnected",
						login: state.connected ? "alice" : "",
						checked_at: 0,
						expires_at: 0,
					};
				return answers[proc] ?? null;
			}),
		);
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(trpcOk(results)),
		});
	});
	return state;
}

async function openDownload(page: Page, surface: "channel" | "watch") {
	await page
		.getByRole("button", {
			name: surface === "channel" ? "Download" : "Record live",
			exact: true,
		})
		.click();
	return page.getByRole("dialog");
}

for (const surface of ["channel", "watch"] as const) {
	for (const followed of [true, false]) {
		test(`${surface} download checks the broadcaster and codec-specific quality (followed=${followed})`, async ({
			page,
		}) => {
			const state = await downloadServer(page, followed);
			state.connected = true;
			let releaseLive!: () => void;
			state.beforeLive = () =>
				new Promise<void>((resolve) => {
					releaseLive = resolve;
				});
			let releaseCodec!: () => void;
			const codecResponse = new Promise<void>((resolve) => {
				releaseCodec = resolve;
			});
			let codecRequested = false;
			state.beforeRenditions = async (h264) => {
				if (h264) {
					codecRequested = true;
					await codecResponse;
				}
			};
			await page.goto(
				surface === "channel"
					? "/dashboard/channels/chan1"
					: "/dashboard/watch/95",
			);
			const dialog = await openDownload(page, surface);
			const submit = dialog.getByRole("button", {
				name: "Start download",
				exact: true,
			});
			const picker = dialog.getByTestId("live-rendition-picker");
			await expect(dialog).toContainText(
				"Checking whether this channel is live",
			);
			await expect(submit).toBeDisabled();
			expect(state.renditionCalls).toBe(0);
			await expect.poll(() => state.liveCalls).toBe(1);
			state.beforeLive = async () => {};
			releaseLive();
			await expect(picker).toContainText("Choose a quality");
			await expect(submit).toBeDisabled();
			await picker.click();
			await page
				.getByRole("option", { name: "1440p60 · HEVC", exact: true })
				.click();
			await expect(submit).toBeEnabled();
			await dialog.getByRole("checkbox", { name: /Force H.264/ }).check();
			await expect.poll(() => codecRequested).toBe(true);
			await expect(picker).toBeDisabled();
			await expect(submit).toBeDisabled();
			expect(state.downloads).toEqual([]);
			releaseCodec();
			await expect(picker).toContainText("1080p60");
			await expect(submit).toBeEnabled();
			await submit.click();
			await expect(dialog).toHaveCount(0);
			expect(state.downloads).toEqual([
				{
					broadcaster_id: "chan1",
					recording_type: "video",
					force_h264: true,
					quality: "HIGH",
					max_height: 1080,
				},
			]);
		});
	}
}

test("a live channel outside the follows can record audio while video qualities are pending", async ({
	page,
}) => {
	const state = await downloadServer(page);
	let releaseRenditions!: () => void;
	state.beforeRenditions = () =>
		new Promise<void>((resolve) => {
			releaseRenditions = resolve;
		});
	await page.goto("/dashboard/watch/95");
	const dialog = await openDownload(page, "watch");
	await expect.poll(() => state.renditionCalls).toBe(1);
	await expect(
		dialog.getByRole("button", { name: "Start download", exact: true }),
	).toBeDisabled();
	await dialog.getByRole("radio", { name: /Audio/ }).click();
	await dialog
		.getByRole("button", { name: "Start download", exact: true })
		.click();
	await expect(dialog).toHaveCount(0);
	expect(state.downloads).toEqual([
		{
			broadcaster_id: "chan1",
			recording_type: "audio",
			quality: "HIGH",
			force_h264: false,
		},
	]);
	releaseRenditions();
});

test("the broadcaster check overrides a stale followed-live snapshot and can be retried", async ({
	page,
}) => {
	const state = await downloadServer(page, true);
	state.live = false;
	await page.goto("/dashboard/channels/chan1");
	const dialog = await openDownload(page, "channel");
	await expect(dialog.getByText("Offline", { exact: true })).toBeVisible();
	await expect(
		dialog.getByRole("button", { name: "Start download", exact: true }),
	).toBeDisabled();
	expect(state.renditionCalls).toBe(0);
	state.live = true;
	await dialog
		.getByRole("button", { name: "Check again", exact: true })
		.click();
	await expect(dialog.getByTestId("live-rendition-picker")).toBeEnabled();
	expect(state.liveCalls).toBe(2);
});

test("connecting through the quality notice refreshes a cached anonymous list on return", async ({
	page,
}) => {
	const state = await downloadServer(page);
	await page.goto("/dashboard/watch/95");
	let dialog = await openDownload(page, "watch");
	const notice = dialog.getByTestId("renditions-session-notice");
	await expect(notice).toBeVisible();
	await dialog.getByTestId("live-rendition-picker").click();
	const anonymousOption = page.getByRole("option", {
		name: "1080p60 · HEVC",
		exact: true,
	});
	await expect(anonymousOption).toBeVisible();
	await expect(
		page.getByRole("option", { name: "1440p60 · HEVC", exact: true }),
	).toHaveCount(0);
	await anonymousOption.click();
	const cachedAt = Date.now();
	const documentOrigin = await page.evaluate(() => performance.timeOrigin);
	await notice.getByRole("link").click();
	await expect(page).toHaveURL(/\/dashboard\/system\/twitch$/);
	await page
		.getByLabel("Twitch auth-token cookie value")
		.fill("test-session-0123456789abcdef");
	await page.getByRole("checkbox").check();
	await page
		.getByRole("button", { name: "Validate and connect", exact: true })
		.click();
	await expect(page.getByText("Connected", { exact: true })).toBeVisible();
	await page.goBack();
	await expect(page).toHaveURL(/\/dashboard\/watch\/95$/);
	expect(await page.evaluate(() => performance.timeOrigin)).toBe(
		documentOrigin,
	);
	dialog = await openDownload(page, "watch");
	await dialog.getByTestId("live-rendition-picker").click();
	await expect(
		page.getByRole("option", { name: "1440p60 · HEVC", exact: true }),
	).toBeVisible();
	await expect(dialog.getByTestId("renditions-session-notice")).toHaveCount(0);
	expect(state.renditionCalls).toBe(2);
	expect(Date.now() - cachedAt).toBeLessThan(30_000);
});

for (const surface of ["channel", "watch"] as const) {
	test(`${surface} requires an explicit ceiling when an unavailable codec cannot preserve a nonstandard quality`, async ({ page }) => {
		const state = await downloadServer(page);
		state.connected = true;
		state.renditionHeight = 936;
		await page.goto(surface === "channel" ? "/dashboard/channels/chan1" : "/dashboard/watch/95");
		const dialog = await openDownload(page, surface);
		const picker = dialog.getByTestId("live-rendition-picker");
		await expect(picker).toContainText("936p60");
		await picker.click();
		await page.getByRole("option", { name: "936p60 · HEVC", exact: true }).click();
		state.emptyRenditions = true;
		await dialog.getByRole("checkbox", { name: /Force H.264/ }).check();
		const fallback = dialog.getByRole("combobox", { name: "Quality", exact: true });
		await expect(fallback).toContainText("Choose a quality");
		const submit = dialog.getByRole("button", { name: "Start download", exact: true });
		await expect(submit).toBeDisabled();
		expect(state.downloads).toEqual([]);
		await fallback.click();
		await page.getByRole("option", { name: "Up to 720p", exact: true }).click();
		await submit.click();
		await expect(dialog).toHaveCount(0);
		expect(state.downloads).toEqual([{
			broadcaster_id: "chan1", recording_type: "video", quality: "MEDIUM", force_h264: true,
		}]);
	});
}
