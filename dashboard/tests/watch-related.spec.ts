import { expect, test } from "@playwright/test";
import { USER_SETTINGS } from "../src/test/playback-settings";
import { fulfillRangeFixture, videoFixture } from "./support/audio";
import { mockSubscription, mockTrpc, procsOf, SESSION, trpcOk } from "./support/trpc";
import { mockWatchPage, recordedAt, userState, videoRecording } from "./support/watch";

test("discovers a return after playback finishes loading and keeps each recording's progress", async ({
	page,
}) => {
	test.setTimeout(60_000);
	const publishChange = await mockSubscription(page, "video.changesLive");
	const members = [
		{
			id: 78,
			position: 1,
			status: "DONE",
			title: "First broadcast",
			job_id: "first",
			started_at: recordedAt,
		},
	];
	const progress = new Map([
		[78, 5],
		[79, 12],
	]);
	let intentStatus = "waiting";
	await page.route("**/api/v1/videos/*/parts/1/stream", (route) =>
		fulfillRangeFixture(route, videoFixture, "video/mp4"),
	);
	await mockTrpc(page, (procs, url) => {
		const input = JSON.parse(new URL(url).searchParams.get("input") ?? "{}");
		return {
			status: 200,
			body: trpcOk(
				procs.map((proc, index) => {
					const id = Number(input[index]?.id ?? input.id ?? 78);
					const member = members.find((row) => row.id === id);
					switch (proc) {
						case "auth.session":
							return SESSION;
						case "video.getById":
							if (!member) throw new Error(`Unknown recording ${id}`);
							return videoRecording(id, 30, {
								title: member.title,
								status: member.status,
								user_state: userState(progress.get(id) ?? 0),
							});
						case "video.relatedRecordings":
							return {
								intent_id: "manual",
								status: intentStatus,
								items: members,
							};
						case "video.timeline":
						case "video.categories":
						case "video.titles":
						case "stream.latestLive":
							return [];
						default:
							return null;
					}
				}),
			),
		};
	});
	await page.route("**/trpc/video.updateWatchProgress*", async (route) => {
		const input = route.request().postDataJSON();
		progress.set(input.video_id, input.position_seconds);
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				result: { data: userState(input.position_seconds) },
			}),
		});
	});
	await page.goto("/dashboard/watch/78");
	await expect(page.locator("video")).toBeAttached();
	await expect
		.poll(() =>
			page.locator("video").evaluate((v: HTMLVideoElement) => v.currentTime),
		)
		.toBeGreaterThanOrEqual(5);
	await expect(page.getByTestId("related-recordings")).toHaveCount(0);
	members.push({
		id: 79,
		position: 2,
		status: "RUNNING",
		title: "Returning broadcast",
		job_id: "second",
		started_at: recordedAt,
	});
	intentStatus = "active";
	publishChange();
	// Hold destination queries so navigation must survive without playback data.
	let releaseDestination!: () => void;
	const destinationReady = new Promise<void>((resolve) => { releaseDestination = resolve; });
	await page.route("**/trpc/**", async (route) => {
		const url = new URL(route.request().url());
		const input = JSON.parse(url.searchParams.get("input") ?? "{}");
		const procs = procsOf(url.toString());
		if (procs.some((proc, index) =>
			(proc === "video.getById" || proc === "video.relatedRecordings") &&
			Number(input[index]?.id ?? input.id) === 79,
		)) await destinationReady;
		await route.fallback();
	});
	await page
		.getByRole("link", { name: "Next recording", exact: true })
		.click({ timeout: 12_000 });
	await expect(page).toHaveURL(/\/watch\/79/);
	await expect(
		page.getByTestId("related-recordings").locator('[aria-current="page"]'),
	).toContainText("Returning broadcast");
	await expect(page.locator("video")).toHaveCount(0);
	await expect(page.getByRole("link", { name: "Previous recording", exact: true })).toBeVisible();
	releaseDestination();
	await expect(page.getByText("This video is not ready to play (status: RUNNING).", { exact: true })).toBeVisible();
	members[1].status = "DONE";
	intentStatus = "waiting";
	publishChange();
	await expect(page.locator("video")).toBeAttached({ timeout: 12_000 });
	await expect
		.poll(() =>
			page.locator("video").evaluate((v: HTMLVideoElement) => v.currentTime),
		)
		.toBeGreaterThanOrEqual(12);
	await page
		.getByRole("link", { name: "Previous recording", exact: true })
		.click();
	await expect(page).toHaveURL(/\/watch\/78/);
	await expect
		.poll(() =>
			page.locator("video").evaluate((v: HTMLVideoElement) => v.currentTime),
		)
		.toBeLessThan(8);
	expect(progress.get(79)).toBeGreaterThanOrEqual(12);
});

test("keeps navigation available on a removed member", async ({ page }) => {
	const items = [
		{
			id: 78,
			position: 1,
			title: "Removed broadcast",
			status: "DONE",
			deleted_at: recordedAt,
			started_at: recordedAt,
			job_id: "first",
		},
		{
			id: 79,
			position: 2,
			title: "Next broadcast",
			status: "RUNNING",
			started_at: recordedAt,
			job_id: "second",
		},
	];
	await mockTrpc(page, (procs) => ({
		status: 200,
		body: trpcOk(
			procs.map((proc) =>
				proc === "auth.session"
					? SESSION
					: proc === "video.getById"
						? videoRecording(78, 30, {
								deleted_at: recordedAt,
								deletion_kind: "manual",
							})
						: proc === "video.relatedRecordings"
							? { intent_id: "manual", status: "active", items }
							: null,
			),
		),
	}));
	await page.goto("/dashboard/watch/78");
	await expect(page.getByTestId("related-recordings")).toContainText("Removed");
	await expect(
		page.getByRole("link", { name: "Next recording", exact: true }),
	).toHaveAttribute("href", "/dashboard/watch/79");
	await expect(page.locator("video")).toHaveCount(0);
});

test("retries a failed related-recording lookup", async ({ page }) => {
	let failing = true;
	await mockTrpc(page, (procs) => ({
		status: 200,
		body: procs.map((proc) => {
			if (proc === "settings.get") return { result: { data: USER_SETTINGS } };
			if (proc === "video.relatedRecordings" && failing) return { error: { message: "Unavailable", code: -32603, data: { code: "INTERNAL_SERVER_ERROR", httpStatus: 500 } } };
			const data = proc === "auth.session" ? SESSION : proc === "video.getById" ? videoRecording(78, 30, { status: "RUNNING" }) : proc === "video.relatedRecordings" ? {
				intent_id: "manual", status: "active", items: [78,79].map((id) => ({ id, job_id: `job-${id}`, position: id-77, title: `Broadcast ${id}`, status: "RUNNING", completion_kind: "complete", started_at: recordedAt })),
			} : [];
			return { result: { data } };
		}),
	}));
	await page.goto("/dashboard/watch/78");
	const retry = page.getByRole("button", { name: "Retry loading related recordings" });
	await expect(retry).toBeVisible({ timeout: 15_000 });
	failing = false;
	await retry.click();
	await expect(page.getByRole("link", { name: "Next recording", exact: true })).toBeVisible();
	await expect(retry).toHaveCount(0);
});

test("shows missing media for a finished recording without parts and refreshes on retry", async ({ page }) => {
	let hasParts = false;
	await mockWatchPage(page, { video: () => videoRecording(78, 30, hasParts ? {} : { parts: [] }) });
	await page.route("**/api/v1/videos/78/parts/1/stream", (route) => fulfillRangeFixture(route, videoFixture, "video/mp4"));
	await page.goto("/dashboard/watch/78");
	const panel = page.getByTestId("media-unavailable");
	await expect(panel).toBeVisible();
	await expect(page.getByTestId("related-recordings")).toHaveCount(0);
	await expect(page.locator("video")).toHaveCount(0);
	hasParts = true;
	await panel.getByRole("button", { name: "Retry" }).click();
	await expect(page.locator("video")).toBeAttached();
	await expect(panel).toHaveCount(0);
});
