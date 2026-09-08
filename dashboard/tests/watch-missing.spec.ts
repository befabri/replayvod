import { expect, test, type Page } from "@playwright/test";
import { audioDurationSeconds, fulfillAudioFixture } from "./support/audio";
import { mockTrpc, trpcOk, validSession } from "./support/trpc";

// The watch page against a recording whose media left storage. The mocked
// stream route answers 404 until the HEAD probe lands and the recording reads
// as tombstoned, matching a server that tombstones before answering that 404.

const recordedAt = "2026-06-05T12:00:00Z";

// "missing" tombstones on the probe, like a recording whose media is entirely
// gone; "missing-partial" never does, like one whose other parts still exist.
type StreamMode = "missing" | "missing-partial" | "outage" | "present";

test.describe("watch page with missing media", () => {
	test.use({ viewport: { width: 1536, height: 768 } });

	test("flips to the removed page once the server tombstoned the recording", async ({
		page,
	}) => {
		const state = await mockWatchMissing(page, "missing");
		await page.goto("/dashboard/watch/95");

		// The panel only lives between the probe and the refetch it triggers, so
		// the end state is what this case pins.
		await expect(
			page.getByText(
				"This recording's files are no longer in storage, so it was removed from the library.",
			),
		).toBeVisible({ timeout: 30_000 });
		await expect(page.getByRole("link", { name: /Back to history/ })).toBeVisible();
		expect(state.probes).toBeGreaterThan(0);
		await expect(page.getByTestId("media-unavailable")).toHaveCount(0);
	});

	test("keeps the file-missing panel up when the server leaves the recording in place", async ({
		page,
	}) => {
		const state = await mockWatchMissing(page, "missing-partial");
		await page.goto("/dashboard/watch/95");

		const panel = page.getByTestId("media-unavailable");
		await expect(panel).toBeVisible({ timeout: 30_000 });
		await expect(panel).toContainText("This file is no longer in storage.");
		await expect(panel.getByRole("link", { name: "View history" })).toBeVisible();
		await expect(panel.getByRole("button", { name: "Retry" })).toBeVisible();
		expect(state.probes).toBeGreaterThan(0);
		await expect(page.getByRole("link", { name: /Back to history/ })).toHaveCount(0);
	});

	test("offers a retry after a storage outage and plays once storage is back", async ({
		page,
	}) => {
		const state = await mockWatchMissing(page, "outage");
		await page.goto("/dashboard/watch/95");

		const panel = page.getByTestId("media-unavailable");
		await expect(panel).toBeVisible({ timeout: 30_000 });
		await expect(panel).toContainText("Playback failed.");
		await expect(panel.getByRole("link", { name: "View history" })).toHaveCount(0);

		state.mode = "present";
		await panel.getByRole("button", { name: "Retry" }).click();

		await expect(page.getByTestId("audio-controls")).toBeVisible({ timeout: 10_000 });
		await expect(page.getByRole("slider", { name: "Seek recording" })).toHaveAttribute(
			"aria-valuemax",
			String(audioDurationSeconds),
		);
	});
});

async function mockWatchMissing(page: Page, mode: StreamMode) {
	const state = { mode, removed: false, probes: 0 };

	// The preview server answers 404 for anything under /api, so every media
	// route the page needs is mocked; the audio waveform is the one the watch
	// page requests on its own.
	await page.route("**/api/v1/videos/95/waveform", async (route) => {
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				duration_seconds: audioDurationSeconds,
				peaks: Array.from({ length: 96 }, () => 0.5),
			}),
		});
	});

	await page.route("**/api/v1/videos/95/parts/1/stream", async (route) => {
		if (state.removed) {
			await route.fulfill({ status: 410, body: "video deleted" });
			return;
		}
		switch (state.mode) {
			case "present":
				await fulfillAudioFixture(route);
				return;
			case "outage":
				await route.fulfill({ status: 503, body: "storage unavailable" });
				return;
			case "missing":
			case "missing-partial":
				if (route.request().method() === "HEAD") {
					state.probes += 1;
					state.removed = state.mode === "missing";
				}
				await route.fulfill({ status: 404, body: "video file missing" });
		}
	});

	await mockTrpc(page, (procs) => {
		const session = validSession(procs, "");
		if (session) return session;
		if (procs.includes("video.getById")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) =>
						proc === "video.getById"
							? state.removed
								? {
										...missingAudioVideo(),
										deleted_at: "2026-09-06T16:00:00Z",
										deletion_kind: "missing",
										parts: [],
									}
								: missingAudioVideo()
							: null,
					),
				),
			};
		}
		if (procs.includes("video.timeline")) {
			return {
				status: 200,
				body: trpcOk(procs.map((proc) => (proc === "video.timeline" ? [] : null))),
			};
		}
		if (procs.includes("video.categories") || procs.includes("video.titles")) {
			return {
				status: 200,
				body: trpcOk(
					procs.map((proc) =>
						proc === "video.categories" || proc === "video.titles" ? [] : null,
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
									broadcaster_login: "gonechannel",
									broadcaster_name: "Gone Channel",
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
							? { total: 1, total_size: 99_000_000, total_duration_seconds: audioDurationSeconds }
							: null,
					),
				),
			};
		}
		if (procs.includes("stream.latestLive")) {
			return {
				status: 200,
				body: trpcOk(procs.map((proc) => (proc === "stream.latestLive" ? [] : null))),
			};
		}
		return null;
	});

	return state;
}

function missingAudioVideo() {
	return {
		id: 95,
		job_id: "job-95",
		filename: "gone-audio",
		display_name: "Gone Channel",
		title: "Ranked climb, day 3",
		status: "DONE",
		completion_kind: "complete",
		truncated: false,
		quality: "audio_only",
		codec: "aac",
		is_audio_only: true,
		broadcaster_id: "chan1",
		broadcaster_login: "gonechannel",
		broadcaster_name: "Gone Channel",
		profile_image_url: "",
		viewer_count: 0,
		language: "en",
		duration_seconds: audioDurationSeconds,
		size_bytes: 99_000_000,
		start_download_at: recordedAt,
		downloaded_at: recordedAt,
		parts: [
			{
				part_index: 1,
				filename: "gone-audio-part1.m4a",
				quality: "audio_only",
				codec: "aac",
				segment_format: "fmp4",
				duration_seconds: audioDurationSeconds,
				size_bytes: 99_000_000,
				start_media_seq: 0,
				end_media_seq: 12,
			},
		],
		playback_artifact: { status: "unavailable", updated_at: recordedAt },
	};
}
