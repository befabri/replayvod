import { expect, waitFor, within } from "storybook/test";
import preview from "#.storybook/preview";
import { recordingPosterURL } from "@/features/videos/thumbnail";
import i18n from "@/i18n";
import { allOf } from "@/test/exhaustive";
import { makeVideo, VIDEO_STATES } from "@/test/fixtures";
import type { WatchLayout } from "./WatchHeaderActions";
import { WatchPageSkeleton } from "./WatchPageSkeleton";

const LAYOUTS = allOf<WatchLayout>({ aside: true, wide: true });

const VIDEO = makeVideo(0);

const meta = preview.meta({
	title: "Features/Videos/WatchPageSkeleton",
	component: WatchPageSkeleton,
	args: { layout: "aside" as const },
	argTypes: {
		layout: { control: "inline-radio", options: LAYOUTS },
		video: { control: false },
	},
	parameters: { layout: "padded" },
	globals: { viewport: { value: "desktop" } },
});

export const Aside = meta.story({
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
	},
});

export const Wide = meta.story({
	args: { layout: "wide" },
});

export const CachedPoster = meta.story({
	args: { video: VIDEO },
	play: async ({ canvas, canvasElement }) => {
		await waitFor(() =>
			expect(
				canvasElement.querySelector(`img[src="${recordingPosterURL(VIDEO)}"]`),
			).toBeVisible(),
		);
		await expect(VIDEO.tags).toBeUndefined();
		await expect(canvas.queryByTestId("watch-tags-skeleton")).toBeNull();
	},
});

export const CachedTags = meta.story({
	args: { video: makeVideo(4) },
	play: async ({ args, canvas }) => {
		const tags = args.video?.tags ?? [];
		await expect(tags.length).toBeGreaterThan(0);
		const row = canvas.getByTestId("watch-tags-skeleton");
		await expect(row.children).toHaveLength(tags.length);
		for (const [index, tag] of tags.entries()) {
			const text = within(row.children[index] as HTMLElement).getByText(
				tag.name,
			);
			await expect(text).not.toBeVisible();
			await expect(text.closest("[aria-hidden='true']")).not.toBeNull();
		}
	},
});

export const CachedAudioPoster = meta.story({
	args: { video: makeVideo(1, VIDEO_STATES.audioOnly) },
	play: async ({ canvas }) => {
		await expect(canvas.getByTestId("audio-thumbnail")).toBeVisible();
	},
});

export const PosterUnavailable = meta.story({
	args: { video: makeVideo(2, VIDEO_STATES.noThumbnail) },
	play: async ({ canvas, canvasElement }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
		await expect(canvas.queryByTestId("player-poster")).toBeNull();
		await expect(canvasElement.querySelector("img")).toBeNull();
	},
});

export const AudioWithoutThumbnail = meta.story({
	args: {
		video: makeVideo(1, { ...VIDEO_STATES.audioOnly, thumbnail: undefined }),
	},
	play: async ({ canvas, canvasElement }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
		await expect(canvas.queryByTestId("audio-thumbnail")).toBeNull();
		await expect(canvasElement.querySelector("img")).toBeNull();
	},
});
