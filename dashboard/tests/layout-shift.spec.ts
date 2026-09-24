import { expect, type Page, test } from "@playwright/test";
import { USER_SETTINGS } from "../src/test/playback-settings";
import {
	forgetLandmarks,
	landmarkKeys,
	landmarkMoves,
	watchLandmarks,
} from "./support/layout";
import { mockTrpc, SESSION, trpcOk } from "./support/trpc";
import { videoRecording } from "./support/watch";

// Loading states stand in for the page they precede. These specs hold the
// page's data, record where its landmarks sit on every frame from the first
// paint, release the data, and fail if anything that was on screen moves:
// a placeholder of the wrong size, a section that appears above the content
// or vanishes, or a virtualized row painted at an estimate.

const NOW = "2026-06-01T12:00:00Z";
const BOX_ART = `<svg xmlns="http://www.w3.org/2000/svg" width="144" height="192"><rect width="144" height="192" fill="#7a5cff"/></svg>`;

type Answers = Record<string, () => unknown>;

async function holdPage(
	page: Page,
	role: "owner" | "viewer",
	answers: Answers,
	held: RegExp,
) {
	let release!: () => void;
	const gate = new Promise<void>((resolve) => {
		release = resolve;
	});
	await page.setViewportSize({ width: 1536, height: 900 });
	await page.route("**/box-art*", (route) =>
		route.fulfill({ contentType: "image/svg+xml", body: BOX_ART }),
	);
	await mockTrpc(page, (procs) => ({
		status: 200,
		body: trpcOk(
			procs.map((proc) => {
				if (proc === "auth.session") return { ...SESSION, role };
				if (proc === "settings.get") return USER_SETTINGS;
				return answers[proc]?.() ?? null;
			}),
		),
	}));
	await page.route("**/trpc/**", async (route) => {
		if (held.test(route.request().url())) await gate;
		await route.fallback();
	});
	return release;
}

async function settle(page: Page, release: () => void) {
	await page.waitForTimeout(800);
	release();
	await page.waitForTimeout(1200);
}

function schedule(id: number, overrides: Record<string, unknown> = {}) {
	return {
		id,
		broadcaster_id: `b${id}`,
		requested_by: "u1",
		requested_from_name: "",
		recording_type: "video",
		quality: "HIGH",
		force_h264: false,
		has_min_viewers: false,
		has_categories: false,
		has_tags: false,
		is_delete_rediff: false,
		is_disabled: false,
		last_triggered_at: NOW,
		trigger_count: 3,
		created_at: NOW,
		updated_at: NOW,
		categories: [],
		tags: [],
		...overrides,
	};
}

const SCHEDULE_ANSWERS: Answers = {
	"schedule.list": () => ({ data: [schedule(1), schedule(2), schedule(3)] }),
	"schedule.pauseState": () => ({ paused: false }),
	"schedule.requests": () => ({ items: [] }),
	"schedule.myRequests": () => ({ items: [] }),
	"channel.getById": () => ({
		broadcaster_id: "b1",
		broadcaster_login: "pixelharbor",
		broadcaster_name: "PixelHarbor",
		profile_image_url: "",
		view_count: 0,
		created_at: NOW,
		updated_at: NOW,
	}),
};

const CATEGORY = {
	id: "509658",
	name: "Just Chatting",
	box_art_url: "https://static-cdn.example/box-art-{width}x{height}.svg",
	description: "Hang out and talk with the streamer and their chat.",
	created_at: NOW,
	updated_at: NOW,
};

const VIDEOS = [1, 2, 3, 4, 5, 6].map((id) =>
	videoRecording(id, 3600, { thumbnail: "" }),
);

test.describe("pages keep their layout when the data arrives", () => {
	test("schedules, for an admin with no pending requests", async ({ page }) => {
		const release = await holdPage(
			page,
			"owner",
			SCHEDULE_ANSWERS,
			/schedule\.|channel\.getById/,
		);
		await watchLandmarks(page, {
			byText: ["main h1", "main h2", "main span.text-xs"],
			byOrder: ["main .lg\\:grid-cols-\\[repeat\\(auto-fit\\,minmax\\(600px\\,1fr\\)\\)\\] > *"],
		});
		await page.goto("/dashboard/schedules");
		await settle(page, release);

		expect(await landmarkMoves(page)).toEqual([]);
		const keys = [...(await landmarkKeys(page))];
		expect(keys.filter((key) => key.includes("Channel requests"))).toEqual([]);
	});

	test("schedules, for a viewer", async ({ page }) => {
		const release = await holdPage(
			page,
			"viewer",
			SCHEDULE_ANSWERS,
			/schedule\.|channel\.getById/,
		);
		await watchLandmarks(page, {
			byText: ["main h1", "main h2"],
			byOrder: ["main .lg\\:grid-cols-\\[repeat\\(auto-fit\\,minmax\\(600px\\,1fr\\)\\)\\] > *"],
		});
		await page.goto("/dashboard/schedules");
		await settle(page, release);

		expect(await landmarkMoves(page)).toEqual([]);
	});

	test("a category and its recordings", async ({ page }) => {
		const release = await holdPage(
			page,
			"owner",
			{
				"category.getDetail": () => ({
					id: "509658",
					name: "Just Chatting",
					box_art_url: "https://static-cdn.example/box-art-{width}x{height}.svg",
					description: "Hang out and talk with the streamer and their chat.",
					video_count: VIDEOS.length,
					total_size: 12_000_000_000,
					created_at: NOW,
					updated_at: NOW,
				}),
				"video.byCategory": () => ({ items: VIDEOS }),
			},
			/category\.getDetail|video\.byCategory/,
		);
		await watchLandmarks(page, {
			byText: ["main h2"],
			byOrder: ["main [data-index]", "main [data-slot='loading-state'] > *"],
		});
		await page.goto("/dashboard/categories/509658");
		await settle(page, release);

		expect(await landmarkMoves(page)).toEqual([]);
	});

	// Coming back to a category serves it from the cache, so the grid mounts
	// with the route itself. On a slow machine its first frame must already
	// have the final column count and row heights.
	test("a category opened again from the list on a slow machine", async ({
		page,
	}) => {
		const release = await holdPage(
			page,
			"owner",
			{
				"category.listPage": () => ({ items: [CATEGORY] }),
				"category.listWithVideos": () => ({ items: [CATEGORY] }),
				"category.getDetail": () => ({
					...CATEGORY,
					video_count: VIDEOS.length,
					total_size: 12_000_000_000,
				}),
				"video.byCategory": () => ({ items: VIDEOS }),
			},
			/^$/,
		);
		release();
		await watchLandmarks(page, {
			byText: ["main h1", "main h2"],
			byOrder: ["main [data-index]"],
		});
		await page.goto(`/dashboard/categories/${CATEGORY.id}`);
		await page.getByRole("link", { name: "Resume fixture" }).first().waitFor();
		await page
			.locator("main")
			.getByRole("link", { name: "Categories", exact: true })
			.click();
		await page.getByRole("link", { name: CATEGORY.name }).first().waitFor();
		const cdp = await page.context().newCDPSession(page);
		await cdp.send("Emulation.setCPUThrottlingRate", { rate: 6 });
		await forgetLandmarks(page);

		await page.getByRole("link", { name: CATEGORY.name }).first().click();
		await page.getByRole("link", { name: "Resume fixture" }).first().waitFor();
		await page.waitForTimeout(1500);

		expect(await landmarkMoves(page)).toEqual([]);
	});

	test("the recordings library", async ({ page }) => {
		const release = await holdPage(
			page,
			"owner",
			{
				"video.statistics": () => ({
					total: 81,
					total_size: 34_000_000_000,
					channels: 49,
					this_week: 6,
					unwatched: 12,
					watch_later: 3,
					continue_watching: 2,
					by_status: [],
				}),
				"video.listPage": () => ({ items: VIDEOS }),
			},
			/video\.(statistics|listPage)/,
		);
		await watchLandmarks(page, {
			byText: ["main h1"],
			byOrder: ["main [role=tablist]", "main [data-index]"],
		});
		await page.goto("/dashboard/videos");
		await settle(page, release);

		expect(await landmarkMoves(page)).toEqual([]);
	});
});
