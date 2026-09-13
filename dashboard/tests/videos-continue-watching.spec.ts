import { expect, test } from "@playwright/test";
import { mockSubscription, mockTrpc, SESSION, trpcOk } from "./support/trpc";
import { userState, videoRecording } from "./support/watch";

test("Continue watching keeps server quality matches with a displayed FPS suffix", async ({
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
					inputs.push(batch[index]);
					return {
						items: [
							videoRecording(41, 3600, {
								quality: "1080p60",
								user_state: userState(900),
							}),
						],
					};
				}),
			),
		};
	});
	await page.goto("/dashboard/videos?tab=continue_watching&quality=1080p");
	await expect(
		page.getByRole("link", { name: "Watch Resume fixture" }),
	).toBeVisible();
	expect(inputs).toContainEqual(
		expect.objectContaining({
			continue_watching_only: true,
			quality: "1080p",
		}),
	);
});

test("Continue watching filters the library and supports table view and reload", async ({
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
					if (proc === "video.statistics")
						return {
							total: 100,
							total_size: 0,
							channels: 1,
							continue_watching: 75,
						};
					if (proc !== "video.listPage") return null;
					const input = batch[index];
					inputs.push(input);
					return {
						items: input.continue_watching_only
							? [videoRecording(41, 3600, { user_state: userState(900) })]
							: [videoRecording(42, 3600, { title: "Unstarted video" })],
					};
				}),
			),
		};
	});
	await page.goto("/dashboard/videos");
	await expect(
		page.getByRole("link", { name: "Watch Unstarted video" }),
	).toBeVisible();
	await expect(
		page.getByRole("tab", { name: "Continue watching 75" }),
	).toBeVisible();
	await page.getByRole("tab", { name: "Continue watching 75" }).click();
	await expect(page).toHaveURL(/tab=continue_watching/);
	await expect(
		page.getByRole("link", { name: "Watch Resume fixture" }),
	).toBeVisible();
	await expect(
		page.getByRole("link", { name: "Watch Unstarted video" }),
	).toHaveCount(0);
	await expect(page.getByTestId("video-card-progress")).toHaveAttribute(
		"aria-valuenow",
		"25",
	);
	await page.getByRole("button", { name: "Table", exact: true }).click();
	await expect(page.getByRole("table")).toBeVisible();
	await page.reload();
	await expect(
		page.getByRole("tab", { name: "Continue watching 75" }),
	).toHaveAttribute("aria-selected", "true");
	await expect(page.getByRole("table")).toBeVisible();
	expect(inputs.some((input) => input.continue_watching_only === true)).toBe(
		true,
	);
});

test("Continue watching explains an empty library", async ({ page }) => {
	await mockTrpc(page, (procs) => ({
		status: 200,
		body: trpcOk(
			procs.map((proc) =>
				proc === "auth.session"
					? SESSION
					: proc === "video.listPage"
						? { items: [] }
						: proc === "video.statistics"
							? { total: 0, total_size: 0, channels: 0, continue_watching: 0 }
							: null,
			),
		),
	}));
	await page.goto("/dashboard/videos?tab=continue_watching");
	await expect(
		page.getByRole("tab", { name: "Continue watching 0" }),
	).toBeVisible();
	await expect(
		page.getByText(
			"No videos to continue. Start watching a video to see it here.",
		),
	).toBeVisible();
});

test("Continue watching refreshes its count and rows after video removal", async ({
	page,
}) => {
	const publishChange = await mockSubscription(page, "video.changesLive");
	let removed = false;
	await mockTrpc(page, (procs) => ({
		status: 200,
		body: trpcOk(
			procs.map((proc) => {
				if (proc === "auth.session") return SESSION;
				if (proc === "video.statistics")
					return {
						total: removed ? 0 : 1,
						total_size: 0,
						channels: 1,
						continue_watching: removed ? 0 : 1,
					};
				if (proc === "video.listPage")
					return {
						items: removed
							? []
							: [videoRecording(41, 3600, { user_state: userState(900) })],
					};
				return null;
			}),
		),
	}));
	await page.goto("/dashboard/videos?tab=continue_watching");
	await expect(
		page.getByRole("tab", { name: "Continue watching 1" }),
	).toBeVisible();
	await expect(
		page.getByRole("link", { name: "Watch Resume fixture" }),
	).toBeVisible();
	removed = true;
	publishChange();
	await expect(
		page.getByRole("tab", { name: "Continue watching 0" }),
	).toBeVisible();
	await expect(
		page.getByRole("link", { name: "Watch Resume fixture" }),
	).toHaveCount(0);
	await expect(
		page.getByText(
			"No videos to continue. Start watching a video to see it here.",
		),
	).toBeVisible();
});
