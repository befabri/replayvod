import { expect, fn, screen, waitFor, within } from "storybook/test";
import preview from "#.storybook/preview";
import type { SetWatchLaterInput } from "@/api/generated/trpc";
import { formatBytes } from "@/features/videos/format";
import i18n from "@/i18n";
import {
	makeUserState,
	makeVideo,
	makeVideos,
	VIDEO_SNAPSHOTS,
	VIDEO_STATES,
	videoHandlers,
} from "@/test/fixtures";
import { trpcParameters } from "@/test/trpc-mock";
import { VideoCard } from "./VideoCard";
import { VideoCardSkeleton } from "./VideoCardSkeleton";

const VIDEOS = makeVideos(8);

const setWatchLater = fn(({ watch_later }: SetWatchLaterInput) =>
	makeUserState({ watch_later }),
);

const meta = preview.meta({
	title: "Features/Videos/VideoCard",
	component: VideoCard,
	args: { video: VIDEOS[0], canManage: false },
	parameters: trpcParameters(videoHandlers(VIDEOS)),
	decorators: [
		(Story) => (
			<div className="w-80">
				<Story />
			</div>
		),
	],
});

export const Done = meta.story();

export const Resumable = meta.story({
	args: { video: makeVideo(1, VIDEO_STATES.resumable) },
	play: async ({ canvas }) => {
		await expect(canvas.getByTestId("video-card-progress")).toBeVisible();
	},
});

export const InWatchLater = meta.story({
	args: { video: makeVideo(2, VIDEO_STATES.watchLater) },
});

export const Recording = meta.story({
	args: { video: makeVideo(3, VIDEO_STATES.recording) },
});

export const Queued = meta.story({
	args: { video: makeVideo(4, VIDEO_STATES.queued) },
});

export const Failed = meta.story({
	args: { video: makeVideo(5, VIDEO_STATES.failed) },
});

export const Cancelled = meta.story({
	args: { video: makeVideo(6, VIDEO_STATES.cancelled) },
});

export const Partial = meta.story({
	args: { video: makeVideo(7, VIDEO_STATES.partial) },
});

export const Truncated = meta.story({
	args: { video: makeVideo(8, VIDEO_STATES.truncated) },
});

export const Archive = meta.story({
	args: { video: makeVideo(9, VIDEO_STATES.archive) },
	play: async ({ canvas }) => {
		await expect(canvas.getByTestId("video-card-archive")).toBeVisible();
	},
});

export const AudioOnly = meta.story({
	args: { video: makeVideo(11, VIDEO_STATES.audioOnly) },
});

export const NoThumbnail = meta.story({
	args: { video: makeVideo(10, VIDEO_STATES.noThumbnail) },
	play: async ({ canvas }) => {
		await expect(
			await canvas.findByText(i18n.t("videos.no_thumbnail")),
		).toBeVisible();
	},
});

export const ToggleWatchLater = meta.story({
	parameters: trpcParameters({ video: { setWatchLater } }),
	play: async ({ canvas, userEvent }) => {
		const button = canvas.getByRole("button", {
			name: i18n.t("videos.watch_later.label"),
		});
		await expect(button).toHaveAttribute("aria-pressed", "false");
		await userEvent.click(button);
		await waitFor(() =>
			expect(setWatchLater).toHaveBeenCalledWith({
				video_id: VIDEOS[0].id,
				watch_later: true,
			}),
		);
		await expect(button).toHaveAttribute("aria-pressed", "true");
		await expect(button).toHaveAccessibleName(
			i18n.t("videos.watch_later.label"),
		);
		await expect(button).toHaveAttribute(
			"title",
			i18n.t("videos.watch_later.remove"),
		);
	},
});

export const Manageable = meta.story({
	args: { canManage: true },
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(
			canvas.getByRole("button", { name: i18n.t("videos.remove") }),
		);
		await waitFor(() => expect(screen.getByRole("dialog")).toBeVisible());
		await expect(
			screen.getByText(i18n.t("videos.remove_confirm_title")),
		).toBeVisible();
	},
});

export const HoverPreview = meta.story({
	play: async ({ canvas, canvasElement, userEvent }) => {
		const snapshot = VIDEO_SNAPSHOTS[0].replace(/^thumbnails\//, "");
		const media = canvas.getByTestId("video-card-play").parentElement;
		if (!media) throw new Error("VideoCard media area not found");
		await userEvent.hover(media);
		await waitFor(() =>
			expect(
				[...canvasElement.querySelectorAll("img")].some((img) =>
					img.getAttribute("src")?.endsWith(snapshot),
				),
			).toBe(true),
		);
	},
});

// The grid's loading cards are the card's own layout with placeholders, so
// the thumbnail, title and channel lines land where the recording's will.
export const SkeletonMatchesCard = meta.story({
	render: (args) => (
		<div className="grid w-[36rem] gap-8">
			<div data-testid="skeleton">
				<VideoCardSkeleton />
			</div>
			<div data-testid="loaded">
				<VideoCard {...args} />
			</div>
		</div>
	),
	play: async ({ canvas }) => {
		const skeleton = canvas.getByTestId("skeleton");
		const loaded = canvas.getByTestId("loaded");
		const card = within(loaded);
		const video = VIDEOS[0];
		const channelLinks = card.getAllByRole("link", {
			name: video.display_name,
		});
		const loadedParts = {
			media: loaded.querySelector(".aspect-video"),
			title: card.getByRole("link", { name: video.title }),
			avatar: channelLinks[0],
			channel: channelLinks[1],
			meta: card.getByText(formatBytes(video.size_bytes)).parentElement,
		};
		const [media, title, avatar, channel, meta] = [
			...skeleton.querySelectorAll('[data-slot="skeleton"]'),
		];
		const skeletonParts = {
			media,
			title: title?.parentElement,
			avatar,
			channel: channel?.parentElement,
			meta: meta?.parentElement,
		};
		const place = (element: Element | null | undefined, root: Element) => {
			if (!element) return null;
			const box = element.getBoundingClientRect();
			return {
				top: Math.round(box.top - root.getBoundingClientRect().top),
				height: Math.round(box.height),
			};
		};
		for (const part of Object.keys(
			loadedParts,
		) as (keyof typeof loadedParts)[]) {
			await expect({ part, ...place(skeletonParts[part], skeleton) }).toEqual({
				part,
				...place(loadedParts[part], loaded),
			});
		}
		await expect(Math.round(skeleton.getBoundingClientRect().height)).toBe(
			Math.round(loaded.getBoundingClientRect().height),
		);
	},
});
