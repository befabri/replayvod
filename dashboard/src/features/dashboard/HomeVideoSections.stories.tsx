import { expect, fn, waitFor } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import {
	FIXTURE_NOW,
	makeUserState,
	makeVideos,
	videoHandlers,
} from "@/test/fixtures";
import { USER_SETTINGS } from "@/test/playback-settings";
import {
	mergeTrpcHandlers,
	neverResolves,
	trpcError,
	trpcParameters,
} from "@/test/trpc-mock";
import { HomeVideoSections } from "./HomeVideoSections";

const LATEST = makeVideos(5);
const RESUMABLE = makeVideos(3, () => ({
	duration_seconds: 5400,
	user_state: makeUserState({
		last_position_seconds: 2000,
		watched_at: new Date(FIXTURE_NOW - 3_600_000).toISOString(),
	}),
}));

const HANDLERS = mergeTrpcHandlers(videoHandlers([...LATEST, ...RESUMABLE]), {
	video: {
		listPage: () => ({ items: LATEST }),
		continueWatching: () => RESUMABLE,
	},
});

let settingsCalls = 0;
const failFirstSettingsLoad = fn(() => {
	settingsCalls += 1;
	if (settingsCalls === 1) {
		throw trpcError("INTERNAL_SERVER_ERROR", 500, "database is locked");
	}
	return USER_SETTINGS;
});

const meta = preview.meta({
	title: "Features/Dashboard/HomeVideoSections",
	component: HomeVideoSections,
	parameters: { layout: "padded", ...trpcParameters(HANDLERS) },
});

export const Default = meta.story({
	play: async ({ canvas }) => {
		const latest = await canvas.findByTestId("latest-recordings");
		const resume = await canvas.findByTestId("continue-watching");
		await expect(latest).toBeVisible();
		await expect(resume).toBeVisible();
		await expect(
			latest.compareDocumentPosition(resume) & Node.DOCUMENT_POSITION_FOLLOWING,
		).toBeTruthy();
	},
});

export const SettingsLoading = meta.story({
	parameters: trpcParameters({ settings: { get: neverResolves } }),
	play: async ({ canvas }) => {
		await expect(
			await canvas.findByRole("heading", {
				name: i18n.t("dashboard.latest_recordings"),
			}),
		).toBeVisible();
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
	},
});

// Both sections read the viewer's playback settings. One failure is one
// message with one Retry, not the same alert repeated in each section.
export const SettingsFailed = meta.story({
	parameters: trpcParameters({ settings: { get: failFirstSettingsLoad } }),
	beforeEach: () => {
		settingsCalls = 0;
		failFirstSettingsLoad.mockClear();
	},
	play: async ({ canvas, userEvent }) => {
		const alert = await canvas.findByRole("alert");
		await expect(alert).toHaveTextContent(i18n.t("settings.failed_to_load"));
		await expect(canvas.getAllByRole("alert")).toHaveLength(1);
		await expect(
			canvas.getAllByRole("button", { name: i18n.t("common.retry") }),
		).toHaveLength(1);

		await userEvent.click(
			canvas.getByRole("button", { name: i18n.t("common.retry") }),
		);

		await expect(await canvas.findByTestId("continue-watching")).toBeVisible();
		await expect(await canvas.findByTestId("latest-recordings")).toBeVisible();
		await waitFor(() => expect(canvas.queryByRole("alert")).toBeNull());
	},
});
