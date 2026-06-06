import { Buffer } from "node:buffer";
import { expect, test, type Page, type Route } from "@playwright/test";
import { mockTrpc, trpcOk, validSession } from "./support/trpc";

const recordedAt = "2026-06-05T12:00:00Z";
const audioDurationSeconds = 4;
const audioFixture = Buffer.from(
	[
		"AAAAHGZ0eXBNNEEgAAACAE00QSBpc29taXNvMgAABbNtb292AAAAbG12aGQAAAAAAAAAAAAAAAAAAAPoAAAPoAABAAABAAAA",
		"AAAAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAC",
		"AAAE3XRyYWsAAABcdGtoZAAAAAMAAAAAAAAAAAAAAAEAAAAAAAAPoAAAAAAAAAAAAAAAAQEAAAAAAQAAAAAAAAAAAAAAAAAA",
		"AAEAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAACRlZHRzAAAAHGVsc3QAAAAAAAAAAQAAD6AAAAQAAAEAAAAABFVtZGlh",
		"AAAAIG1kaGQAAAAAAAAAAAAAAAAAAKxEAAK1EFXEAAAAAAAtaGRscgAAAAAAAAAAc291bgAAAAAAAAAAAAAAAFNvdW5kSGFu",
		"ZGxlcgAAAAQAbWluZgAAABBzbWhkAAAAAAAAAAAAAAAkZGluZgAAABxkcmVmAAAAAAAAAAEAAAAMdXJsIAAAAAEAAAPEc3Ri",
		"bAAAAGpzdHNkAAAAAAAAAAEAAABabXA0YQAAAAAAAAABAAAAAAAAAAAAAQAQAAAAAKxEAAAAAAA2ZXNkcwAAAAADgICAJQAB",
		"AASAgIAXQBUAAAAAAB9AAAAFiQWAgIAFEghW5QAGgICAAQIAAAAgc3R0cwAAAAAAAAACAAAArQAABAAAAAABAAABEAAAABxz",
		"dHNjAAAAAAAAAAEAAAABAAAArgAAAAEAAALMc3RzegAAAAAAAAAAAAAArgAAABUAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAABRzdGNvAAAAAAAAAAEAAAXfAAAAGnNncGQBAAAAcm9sbAAAAAIAAAAB//8AAAAcc2Jn",
		"cAAAAAByb2xsAAAAAQAAAK4AAAABAAAAYnVkdGEAAABabWV0YQAAAAAAAAAhaGRscgAAAAAAAAAAbWRpcmFwcGwAAAAAAAAA",
		"AAAAAAAtaWxzdAAAACWpdG9vAAAAHWRhdGEAAAABAAAAAExhdmY2Mi4xMi4xMDEAAAAIZnJlZQAAAtFtZGF03gIATGF2YzYy",
		"LjI4LjEwMQACMEAOARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAc=",
	].join(""),
	"base64",
);

test.describe("audio watch player", () => {
	test.use({ viewport: { width: 1536, height: 768 } });

	test("can seek away from the visible end progress handle", async ({ page }) => {
		await mockWatchAudio(page);
		await page.goto("/dashboard/watch/65");

		const recordingSlider = page.getByRole("slider", {
			name: "Seek recording",
		});
		await expect(recordingSlider).toBeVisible({ timeout: 30_000 });

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expect
			.poll(async () =>
				page.locator("audio").evaluate((audio: HTMLMediaElement) => audio.ended),
			)
			.toBe(true);
		await expect(recordingSlider).toHaveAttribute(
			"aria-valuenow",
			String(audioDurationSeconds),
		);

		const thumb = page.getByTestId("recording-progress-thumb");
		await expect(thumb).toBeVisible();
		const box = await thumb.boundingBox();
		expect(box).not.toBeNull();
		const railBox = await page.getByTestId("recording-timeline-rail").boundingBox();
		expect(railBox).not.toBeNull();

		const startX = box!.x + box!.width - 1;
		const startY = box!.y + box!.height / 2;
		const earlyX = railBox!.x + railBox!.width * 0.25;
		await expect
			.poll(async () =>
				page.evaluate(
					({ x, y }) => {
						const element = document.elementFromPoint(x, y);
						return {
							label: element?.getAttribute("aria-label") ?? null,
							role: element?.getAttribute("role") ?? null,
						};
					},
					{ x: startX, y: startY },
				),
			)
			.toEqual({ label: "Seek recording", role: "slider" });

		await page.mouse.move(startX, startY);
		await page.mouse.down();
		await page.mouse.move(earlyX, startY, { steps: 6 });
		await page.mouse.up();

		await expect
			.poll(async () => Number(await recordingSlider.getAttribute("aria-valuenow")))
			.toBeLessThan(audioDurationSeconds);
		await expect(recordingSlider).toHaveAttribute("aria-valuenow", "1");
		await expect
			.poll(async () =>
				page.locator("audio").evaluate((audio: HTMLMediaElement) => ({
					currentTime: audio.currentTime,
					ended: audio.ended,
				})),
			)
			.toMatchObject({ ended: false });
		await expect
			.poll(async () =>
				page
					.locator("audio")
					.evaluate((audio: HTMLMediaElement) => audio.currentTime),
			)
			.toBeLessThan(audioDurationSeconds);

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expectAudioPlaybackToStayPastStart(page);
	});

	test("can click an earlier waveform position after playback ended", async ({
		page,
	}) => {
		await mockWatchAudio(page);
		await page.goto("/dashboard/watch/65");

		const recordingSlider = page.getByRole("slider", {
			name: "Seek recording",
		});
		await expect(recordingSlider).toBeVisible({ timeout: 30_000 });

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expect
			.poll(async () =>
				page.locator("audio").evaluate((audio: HTMLMediaElement) => audio.ended),
			)
			.toBe(true);

		const railBox = await page.getByTestId("recording-timeline-rail").boundingBox();
		expect(railBox).not.toBeNull();
		await page.mouse.click(
			railBox!.x + railBox!.width * 0.25,
			railBox!.y + railBox!.height / 2,
		);

		await expect(recordingSlider).toHaveAttribute("aria-valuenow", "1");
		await expect
			.poll(async () =>
				page.locator("audio").evaluate((audio: HTMLMediaElement) => ({
					currentTime: audio.currentTime,
					ended: audio.ended,
				})),
			)
			.toMatchObject({ ended: false });
		await expect
			.poll(async () =>
				page
					.locator("audio")
					.evaluate((audio: HTMLMediaElement) => audio.currentTime),
			)
			.toBeLessThan(audioDurationSeconds);

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expectAudioPlaybackToStayPastStart(page);
	});

	test("resumes from an earlier seek after the user scrubbed to the end", async ({
		page,
	}) => {
		await mockWatchAudio(page);
		await page.goto("/dashboard/watch/65");

		const recordingSlider = page.getByRole("slider", {
			name: "Seek recording",
		});
		await expect(recordingSlider).toBeVisible({ timeout: 30_000 });

		const railBox = await page.getByTestId("recording-timeline-rail").boundingBox();
		expect(railBox).not.toBeNull();
		await page.mouse.click(
			railBox!.x + railBox!.width - 1,
			railBox!.y + railBox!.height / 2,
		);
		await expect(recordingSlider).toHaveAttribute(
			"aria-valuenow",
			String(audioDurationSeconds),
		);

		await page.mouse.click(
			railBox!.x + railBox!.width * 0.25,
			railBox!.y + railBox!.height / 2,
		);
		await expect(recordingSlider).toHaveAttribute("aria-valuenow", "1");

		await page.getByRole("button", { name: "Play", exact: true }).click();
		await expectAudioPlaybackToStayPastStart(page);
		await expect
			.poll(async () =>
				page
					.locator("audio")
					.evaluate((audio: HTMLMediaElement) => audio.currentTime),
			)
			.toBeLessThan(audioDurationSeconds);
	});

	test("does not replay from zero when play is pressed immediately after seeking away from the end", async ({
		page,
	}) => {
		await mockWatchAudio(page);
		await page.goto("/dashboard/watch/65");

		const recordingSlider = page.getByRole("slider", {
			name: "Seek recording",
		});
		await expect(recordingSlider).toBeVisible({ timeout: 30_000 });

		const railBox = await page.getByTestId("recording-timeline-rail").boundingBox();
		expect(railBox).not.toBeNull();

		await page.mouse.click(
			railBox!.x + railBox!.width - 1,
			railBox!.y + railBox!.height / 2,
		);
		await expect(recordingSlider).toHaveAttribute(
			"aria-valuenow",
			String(audioDurationSeconds),
		);

		await page.mouse.click(
			railBox!.x + railBox!.width * 0.25,
			railBox!.y + railBox!.height / 2,
		);
		await page.getByRole("button", { name: "Play", exact: true }).click();

		await expectAudioPlaybackToStayPastStart(page);
	});
});

async function expectAudioPlaybackToStayPastStart(page: Page) {
	const samples = await page.locator("audio").evaluate(async (audio) => {
		const values: number[] = [];
		const startedAt = performance.now();
		while (performance.now() - startedAt < 750) {
			values.push(audio.currentTime);
			await new Promise((resolve) => setTimeout(resolve, 50));
		}
		return values;
	});
	expect(samples.length).toBeGreaterThan(0);
	expect(Math.min(...samples)).toBeGreaterThan(0.5);
	expect(samples.at(-1) ?? 0).toBeGreaterThan(1);
}

async function mockWatchAudio(page: Parameters<typeof mockTrpc>[0]) {
	await page.route("**/api/v1/videos/65/waveform", async (route) => {
		await route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({
					duration_seconds: audioDurationSeconds,
					peaks: Array.from({ length: 96 }, (_, index) =>
						Number((0.2 + Math.abs(Math.sin(index * 0.3)) * 0.75).toFixed(3)),
					),
				}),
		});
	});

	await page.route("**/api/v1/videos/65/parts/1/stream", async (route) => {
		await fulfillAudioFixture(route);
	});

	await mockTrpc(page, (procs) => {
		const session = validSession(procs, "");
		if (session) return session;

		if (procs.includes("video.getById")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) => (proc === "video.getById" ? audioVideo() : null)),
				),
			};
		}
		if (procs.includes("video.timeline")) {
			return {
				status: 200,
				body: trpcOk(procs.map((proc) => (proc === "video.timeline" ? [] : null))),
			};
		}
		if (procs.includes("video.categories")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) =>
						proc === "video.categories"
							? [
									{
										id: "software",
										name: "Software and Game Development",
										started_at: recordedAt,
										duration_seconds: audioDurationSeconds,
									},
								]
							: null,
					),
				),
			};
		}
		if (procs.includes("video.titles")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) =>
						proc === "video.titles"
							? [
									{
										id: 1,
										name: "Steam nukes indies. GL HF.",
										started_at: recordedAt,
										duration_seconds: audioDurationSeconds,
									},
								]
							: null,
					),
				),
			};
		}
		if (procs.includes("channel.getById")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) =>
						proc === "channel.getById"
							? {
									broadcaster_id: "chan1",
									broadcaster_login: "thornityco",
									broadcaster_name: "ThornityCo",
									profile_image_url: "",
									view_count: 0,
									created_at: recordedAt,
									updated_at: recordedAt,
								}
							: null,
					),
				),
			};
		}
		if (procs.includes("video.statisticsByBroadcaster")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) =>
						proc === "video.statisticsByBroadcaster"
								? {
										total: 1,
										total_size: 99_000_000,
										total_duration_seconds: audioDurationSeconds,
									}
								: null,
					),
				),
			};
		}
		if (procs.includes("stream.latestLive")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) => (proc === "stream.latestLive" ? [] : null)),
				),
			};
		}

		return null;
	});
}

function audioVideo() {
	return {
		id: 65,
		job_id: "job-65",
		filename: "steam-nukes-indies",
		display_name: "ThornityCo",
		title: "Steam nukes indies. GL HF.",
		status: "DONE",
		completion_kind: "complete",
		truncated: false,
		quality: "audio_only",
		codec: "aac",
		is_audio_only: true,
		broadcaster_id: "chan1",
		broadcaster_login: "thornityco",
		broadcaster_name: "ThornityCo",
		profile_image_url: "",
		primary_category_id: "software",
		primary_category_name: "Software and Game Development",
		viewer_count: 0,
		language: "en",
		duration_seconds: audioDurationSeconds,
		size_bytes: 99_000_000,
		start_download_at: recordedAt,
		downloaded_at: recordedAt,
		parts: [
			{
				part_index: 1,
				filename: "steam-nukes-indies-part1.m4a",
				quality: "audio_only",
					codec: "aac",
					segment_format: "fmp4",
					duration_seconds: audioDurationSeconds,
					size_bytes: 99_000_000,
					start_media_seq: 0,
				end_media_seq: 12,
			},
		],
		playback_artifact: {
			status: "unavailable",
			updated_at: recordedAt,
		},
	};
}

async function fulfillAudioFixture(route: Route) {
	const range = route.request().headers().range;
	const baseHeaders = {
		"Accept-Ranges": "bytes",
		"Content-Type": "audio/mp4",
	};
	if (route.request().method() === "HEAD") {
		await route.fulfill({
			status: 200,
			headers: {
				...baseHeaders,
				"Content-Length": String(audioFixture.length),
			},
		});
		return;
	}
	if (range) {
		const match = /^bytes=(\d*)-(\d*)$/.exec(range);
		const start = match?.[1] ? Number(match[1]) : 0;
		const requestedEnd = match?.[2] ? Number(match[2]) : audioFixture.length - 1;
		const end = Math.min(requestedEnd, audioFixture.length - 1);
		if (!match || start < 0 || start > end || start >= audioFixture.length) {
			await route.fulfill({
				status: 416,
				headers: {
					...baseHeaders,
					"Content-Range": `bytes */${audioFixture.length}`,
				},
			});
			return;
		}
		const body = audioFixture.subarray(start, end + 1);
		await route.fulfill({
			status: 206,
			headers: {
				...baseHeaders,
				"Content-Length": String(body.length),
				"Content-Range": `bytes ${start}-${end}/${audioFixture.length}`,
			},
			body,
		});
		return;
	}
	await route.fulfill({
		status: 200,
		headers: {
			...baseHeaders,
			"Content-Length": String(audioFixture.length),
		},
		body: audioFixture,
	});
}
