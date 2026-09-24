import { expect, type Locator, type Page, test } from "@playwright/test";
import {
	makeCategoryDetail,
	makeChannelResponse,
	makeSchedules,
	TAGS,
	USER_SETTINGS,
} from "../src/test/fixtures";
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

// settle waits until the held page shows its placeholders and their entrance
// animations have ended, so the watcher records where they rest, then
// releases the data and lets the loaded page paint.
async function settle(page: Page, release: () => void) {
	await page.locator("main [role=status]").first().waitFor();
	await page.waitForFunction(() =>
		document
			.getAnimations()
			.every(
				(animation) =>
					animation.playState !== "running" ||
					animation.effect?.getComputedTiming().endTime === Infinity,
			),
	);
	await page.waitForTimeout(300);
	release();
	await page.waitForTimeout(1200);
}

const SCHEDULE_ANSWERS: Answers = {
	"schedule.list": () => ({ data: makeSchedules(3) }),
	"schedule.pauseState": () => ({ paused: false }),
	"schedule.requests": () => ({ items: [] }),
	"schedule.myRequests": () => ({ items: [] }),
	"channel.getById": () => makeChannelResponse(),
};

// Every other recording carries tags, so a card that grows a tag row once its
// data arrives shows up as a move.
const VIDEOS = [1, 2, 3, 4, 5, 6].map((id) =>
	videoRecording(id, 3600, {
		thumbnail: "",
		tags:
			id % 2 === 0
				? TAGS.slice(0, id / 2).map(({ id, name }) => ({ id, name }))
				: undefined,
	}),
);

const CATEGORY = makeCategoryDetail(0, { video_count: VIDEOS.length });

// rowLayout gives each item's size and its offset from the row's first item,
// so a row that only moves with the content above it still compares equal.
async function rowLayout(items: Locator) {
	const boxes = await items.evaluateAll((elements) =>
		elements.map((element) => element.getBoundingClientRect().toJSON()),
	);
	const [first] = boxes;
	return boxes.map((box) => ({
		x: Math.round(box.x - first.x),
		y: Math.round(box.y - first.y),
		width: Math.round(box.width),
		height: Math.round(box.height),
	}));
}

// The cells of a video grid, skeleton or loaded, in page order. Each loaded
// card takes the key of the skeleton card it replaces, so a skeleton of the
// wrong size fails even in the grid's last row.
const VIDEO_CELLS =
	"main :is([data-slot='loading-state'].grid, [data-index]) > *";

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
				"category.getDetail": () => CATEGORY,
				"video.byCategory": () => ({ items: VIDEOS }),
			},
			/category\.getDetail|video\.byCategory/,
		);
		await watchLandmarks(page, {
			byText: ["main h2"],
			byOrder: ["main [data-index]", "main [data-slot='loading-state'] > *"],
			sizedByOrder: [VIDEO_CELLS],
		});
		await page.goto(`/dashboard/categories/${CATEGORY.id}`);
		await settle(page, release);

		expect(await landmarkMoves(page)).toEqual([]);
	});

	// Coming back to a category serves it from the cache, so the grid mounts
	// with the route itself. On a slow machine its first frame must already
	// have the final column count and row heights.
	test("a category opened again from the list on a slow machine", async ({
		page,
		browserName,
	}) => {
		const release = await holdPage(
			page,
			"owner",
			{
				"category.listPage": () => ({ items: [CATEGORY] }),
				"category.listWithVideos": () => ({ items: [CATEGORY] }),
				"category.getDetail": () => CATEGORY,
				"video.byCategory": () => ({ items: VIDEOS }),
			},
			/^$/,
		);
		release();
		await watchLandmarks(page, {
			byText: ["main h1", "main h2"],
			byOrder: ["main [data-index]"],
			sizedByOrder: [VIDEO_CELLS],
		});
		await page.goto(`/dashboard/categories/${CATEGORY.id}`);
		await page.getByRole("link", { name: "Resume fixture" }).first().waitFor();
		await page
			.locator("main")
			.getByRole("link", { name: "Categories", exact: true })
			.click();
		await page.getByRole("link", { name: CATEGORY.name }).first().waitFor();
		// Only Chromium can slow its CPU down; other browsers replay the same
		// navigation at full speed.
		if (browserName === "chromium") {
			const cdp = await page.context().newCDPSession(page);
			await cdp.send("Emulation.setCPUThrottlingRate", { rate: 6 });
		}
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
			sizedByOrder: [VIDEO_CELLS],
		});
		await page.goto("/dashboard/videos");
		await settle(page, release);

		expect(await landmarkMoves(page)).toEqual([]);
	});

	// A recording opened from the library draws its cached tags while the
	// watch page loads. Each placeholder must take the size and place in the
	// row of the tag that replaces it, wrapping included.
	test("a recording's tags while its watch page loads", async ({ page }) => {
		const tags = TAGS.slice(0, 3).map(({ id, name }) => ({ id, name }));
		const tagged = videoRecording(VIDEOS.length + 1, 3600, {
			thumbnail: "",
			tags,
		});
		const release = await holdPage(
			page,
			"owner",
			{
				"video.listPage": () => ({ items: [...VIDEOS, tagged] }),
				"video.getById": () => tagged,
				"channel.getById": () => makeChannelResponse(),
			},
			/video\.getById/,
		);
		await page.goto("/dashboard/videos");
		await page.locator(`main a[href="/dashboard/watch/${tagged.id}"]`).first().click();

		const placeholders = page
			.getByTestId("watch-tags-skeleton")
			.locator(":scope > *");
		await expect(placeholders).toHaveCount(tags.length);
		const before = await rowLayout(placeholders);
		release();
		const loaded = page
			.locator("main")
			.getByRole("list", { name: "Tags" })
			.getByRole("listitem");
		await expect(loaded).toHaveCount(before.length);

		expect(await rowLayout(loaded)).toEqual(before);
	});

	// Playback settings suspend the page, so while they load it shows its own
	// fallback, which must hold the tab row the loaded page draws.
	test("the recordings library while playback settings load", async ({
		page,
	}) => {
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
			/settings\.get|video\.(statistics|listPage)/,
		);
		await watchLandmarks(page, {
			byText: ["main h1"],
			byOrder: ["main [role=tablist]", "main [data-index]"],
			sizedByOrder: [VIDEO_CELLS],
		});
		await page.goto("/dashboard/videos");
		await settle(page, release);

		expect(await landmarkMoves(page)).toEqual([]);
	});
});
