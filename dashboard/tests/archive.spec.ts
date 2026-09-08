import { expect, type Page, test } from "@playwright/test";
import { procsOf, SESSION, trpcOk } from "./support/trpc";

// The archive page against a stateful mock of the archive.* procedures: paste
// links, browse a channel, and manage the queue, all in a real browser with
// no Go backend. The mock plays the server's part: it answers per input,
// grows the queue on enqueue and shrinks it on dequeue.

type QueueRow = Record<string, unknown>;

function queueRow(id: number, status: "PENDING" | "RUNNING", title: string) {
	return {
		id,
		job_id: `job-${id}`,
		filename: `f-${id}`,
		display_name: "Streamer",
		broadcaster_id: "u1",
		broadcaster_login: "streamer",
		broadcaster_name: "Streamer",
		title,
		status,
		completion_kind: "complete",
		truncated: false,
		quality: "HIGH",
		is_audio_only: false,
		viewer_count: 0,
		language: "en",
		start_download_at: "2026-09-01T10:00:00Z",
		source: "vod",
		twitch_video_id: String(1000 + id),
		broadcast_at: "2026-08-20T20:00:00Z",
	};
}

async function archiveServer(page: Page) {
	const state = {
		queue: [
			queueRow(1, "RUNNING", "Running archive"),
			queueRow(2, "PENDING", "Waiting archive"),
		] as QueueRow[],
		failures: [
			{
				...queueRow(9, "PENDING", "Flaky archive"),
				status: "FAILED",
				error: "hls fetch: transport (attempts=5)",
				next_retry_at: new Date(Date.now() + 3_600_000).toISOString(),
				downloaded_at: "2026-09-01T11:00:00Z",
			},
		] as QueueRow[],
		retryCalls: [] as unknown[],
		cancelRetryCalls: [] as unknown[],
		enqueueCalls: [] as unknown[],
		dequeueCalls: [] as unknown[],
		nextID: 3,
	};
	await page.route("**/trpc/**", async (route) => {
		const req = route.request();
		if ((req.headers().accept ?? "").includes("text/event-stream")) {
			await route.abort();
			return;
		}
		const procs = procsOf(req.url());
		const inputs: unknown[] =
			req.method() === "POST" ? Object.values(req.postDataJSON() ?? {}) : [];
		const values = procs.map((proc, i) => {
			switch (proc) {
				case "auth.session":
					return SESSION;
				case "video.downloadCapacity":
					return { max_concurrent: 2, archive_max_concurrent: 1 };
				case "archive.queue":
					return { queue: state.queue, failures: state.failures };
				case "archive.listChannelVods":
					return {
						channel: {
							broadcaster_id: "u1",
							login: "streamer",
							name: "Streamer",
						},
						vods: [
							{
								id: "501",
								title: "Fresh broadcast",
								url: "https://www.twitch.tv/videos/501",
								type: "archive",
								created_at: "2026-08-30T19:00:00Z",
								duration_seconds: 5400,
								view_count: 10,
							},
							{
								id: "1002",
								title: "Already queued",
								url: "https://www.twitch.tv/videos/1002",
								type: "archive",
								created_at: "2026-08-20T20:00:00Z",
								duration_seconds: 600,
								view_count: 4,
								archived_video_id: 2,
								archived_status: "PENDING",
							},
						],
					};
				case "archive.enqueue": {
					const input = inputs[i] as { vods: string[] };
					state.enqueueCalls.push(input);
					const items = input.vods.map((raw) => {
						const id = raw.match(/(\d+)\/?(?:[?#].*)?$/)?.[1] ?? raw;
						if (id === "1002") {
							return { input: raw, vod_id: id, status: "exists", title: "Already queued", video_id: 2 };
						}
						if (id === "404404") {
							return { input: raw, vod_id: id, status: "not_found", message: "gone" };
						}
						const videoID = state.nextID++;
						state.queue.push(queueRow(videoID, "PENDING", `VOD ${id}`));
						return { input: raw, vod_id: id, status: "queued", title: `VOD ${id}`, video_id: videoID, job_id: `job-${videoID}` };
					});
					return { items };
				}
				case "archive.retry": {
					const input = inputs[i] as { video_id: number };
					state.retryCalls.push(input);
					const failed = state.failures.find((row) => row.id === input.video_id);
					state.failures = state.failures.filter((row) => row.id !== input.video_id);
					if (failed) state.queue.push(queueRow(input.video_id, "PENDING", String(failed.title)));
					return { ok: true };
				}
				case "archive.cancelRetry": {
					const input = inputs[i] as { video_id: number };
					state.cancelRetryCalls.push(input);
					state.failures = state.failures.map((row) =>
						row.id === input.video_id ? { ...row, next_retry_at: undefined } : row,
					);
					return { ok: true };
				}
				case "archive.dequeue": {
					const input = inputs[i] as { video_id: number };
					state.dequeueCalls.push(input);
					state.queue = state.queue.filter((row) => row.id !== input.video_id);
					return { ok: true };
				}
				default:
					return null;
			}
		});
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(trpcOk(values)),
		});
	});
	return state;
}

test.describe("archive", () => {
	test("shows the queue and removes a waiting archive", async ({ page }) => {
		const state = await archiveServer(page);
		await page.goto("/dashboard/archive");
		await expect(
			page.getByRole("heading", { name: "Archive VODs" }),
		).toBeVisible({ timeout: 30_000 });

		const running = page.getByRole("row", { name: /Running archive/ });
		const waiting = page.getByRole("row", { name: /Waiting archive/ });
		await expect(running).toBeVisible();
		await expect(waiting).toBeVisible();
		await expect(running.getByRole("button", { name: "Cancel" })).toBeVisible();
		await waiting.getByRole("button", { name: "Remove" }).click();
		await expect(waiting).toHaveCount(0);
		expect(state.dequeueCalls).toEqual([{ video_id: 2 }]);
	});

	test("shows recent failures and retries one by hand", async ({ page }) => {
		const state = await archiveServer(page);
		await page.goto("/dashboard/archive");
		await expect(
			page.getByRole("heading", { name: "Recent failures" }),
		).toBeVisible({ timeout: 30_000 });

		// The same title appears in the queue once retried, so the failure row is
		// addressed inside its own section.
		const failed = page
			.getByTestId("archive-failures")
			.getByRole("row", { name: /Flaky archive/ });
		await expect(failed).toBeVisible();
		await expect(failed.getByText("hls fetch: transport (attempts=5)")).toBeVisible();
		await expect(failed.getByText(/^Retries /)).toBeVisible();
		await failed.getByRole("button", { name: "Cancel retry" }).click();
		await expect(failed.getByText("Not scheduled")).toBeVisible();
		expect(state.cancelRetryCalls).toEqual([{ video_id: 9 }]);

		await failed.getByRole("button", { name: "Retry now" }).click();
		await expect(failed).toHaveCount(0);
		const queued = page.getByRole("row", { name: /Flaky archive/ });
		await expect(queued).toHaveCount(1);
		await expect(queued.getByText("Waiting")).toBeVisible();
		expect(state.retryCalls).toEqual([{ video_id: 9 }]);
	});

	test("queues pasted links and reports each line", async ({ page }) => {
		const state = await archiveServer(page);
		await page.goto("/dashboard/archive");
		const box = page.getByLabel("VOD links or ids");
		await expect(box).toBeVisible({ timeout: 30_000 });

		await box.fill(
			"https://www.twitch.tv/videos/777?t=1h\nnot a link\n1002\n404404",
		);
		await expect(
			page.getByText("1 line is not a Twitch VOD link and will be skipped."),
		).toBeVisible();
		await page.getByRole("button", { name: "Add 3 VODs to queue" }).click();

		// Scoped to the paste tab: the queue below headers its timestamp column
		// "Queued" too, and it renders as soon as the enqueue invalidates it, so
		// an unscoped match races the refetch.
		const results = page.getByRole("tabpanel", { name: "Paste links" });
		await expect(results.getByText("Queued", { exact: true })).toBeVisible();
		await expect(results.getByText("Already in library")).toBeVisible();
		await expect(results.getByText("Not on Twitch")).toBeVisible();
		await expect(results.getByText("Not a VOD link")).toBeVisible();
		expect(state.enqueueCalls).toEqual([
			{
				vods: ["https://www.twitch.tv/videos/777?t=1h", "1002", "404404"],
				recording_type: "video",
				quality: "HIGH",
				force_h264: false,
			},
		]);
		// The box is cleared and the queue picks up the new row.
		await expect(box).toHaveValue("");
		await expect(page.getByRole("row", { name: /VOD 777/ })).toBeVisible();
	});

	test("browses a channel and archives the selected VODs", async ({ page }) => {
		const state = await archiveServer(page);
		await page.goto("/dashboard/archive");
		await page.getByRole("tab", { name: "Browse a channel" }).click({ timeout: 30_000 });

		const lookup = page.getByRole("button", { name: "Look up" });
		await expect(lookup).toBeDisabled();
		await page.getByPlaceholder("Channel name or twitch.tv link").fill("https://www.twitch.tv/Streamer/videos");
		await lookup.click();

		await expect(page.getByText("Fresh broadcast")).toBeVisible();
		// The VOD already in the queue shows its status and cannot be selected.
		const held = page.getByRole("row", { name: /Already queued/ });
		await expect(held.getByText("Pending")).toBeVisible();
		await expect(held.getByRole("checkbox")).toHaveCount(0);

		await page
			.getByRole("checkbox", { name: "Select every VOD not yet in the library" })
			.click();
		await page.getByRole("button", { name: "Archive 1 selected" }).click();
		await expect(page.getByText("1 VOD queued")).toBeVisible();
		expect(state.enqueueCalls).toEqual([
			{ vods: ["501"], recording_type: "video", quality: "HIGH", force_h264: false },
		]);
		await expect(page.getByRole("row", { name: /VOD 501/ })).toBeVisible();
	});
});
