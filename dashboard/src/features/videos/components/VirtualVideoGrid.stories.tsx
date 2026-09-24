import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import {
	makeVideos,
	VIDEO_STATE_NAMES,
	VIDEO_STATES,
	videoHandlers,
} from "@/test/fixtures";
import { neverResolves, trpcError, trpcParameters } from "@/test/trpc-mock";
import { VideoGridEnd } from "./VideoGridEnd";
import { VideoGridLoading } from "./VideoGridLoading";
import { VirtualVideoGrid } from "./VirtualVideoGrid";

const EVERY_STATE = makeVideos(
	VIDEO_STATE_NAMES.length,
	(index) => VIDEO_STATES[VIDEO_STATE_NAMES[index]],
);
const LIBRARY = makeVideos(500, (index) =>
	index % 7 === 3 ? VIDEO_STATES.resumable : {},
);

const meta = preview.meta({
	title: "Features/Videos/VirtualVideoGrid",
	component: VirtualVideoGrid,
	args: { videos: EVERY_STATE, canManage: false, variant: "compact" as const },
	argTypes: { videos: { control: false } },
	parameters: {
		layout: "padded",
		...trpcParameters(videoHandlers([...EVERY_STATE, ...LIBRARY])),
	},
});

export const EveryState = meta.story({
	play: async ({ canvas }) => {
		const cardsPerTitle = new Map<string, number>();
		for (const video of EVERY_STATE) {
			cardsPerTitle.set(video.title, (cardsPerTitle.get(video.title) ?? 0) + 1);
		}
		for (const [title, cards] of cardsPerTitle) {
			await expect(canvas.getAllByText(title)).toHaveLength(cards);
		}
	},
});

export const Wide = meta.story({
	args: { variant: "wide" },
});

export const Manageable = meta.story({
	args: { canManage: true },
});

export const LargeLibrary = meta.story({
	args: { videos: LIBRARY },
});

export const Loading = meta.story({
	render: () => <VideoGridLoading count={6} className="mt-0" />,
});

export const SettingsLoading = meta.story({
	parameters: trpcParameters({ settings: { get: neverResolves } }),
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
	},
});

export const SettingsFailed = meta.story({
	parameters: trpcParameters({
		settings: {
			get: () => {
				throw trpcError("INTERNAL_SERVER_ERROR", 500, "database is locked");
			},
		},
	}),
	play: async ({ canvas }) => {
		await expect(await canvas.findByRole("alert")).toHaveTextContent(
			i18n.t("settings.failed_to_load"),
		);
		await expect(
			canvas.getByRole("button", { name: i18n.t("common.retry") }),
		).toBeVisible();
	},
});

export const EndOfList = meta.story({
	render: (args) => (
		<>
			<VirtualVideoGrid {...args} videos={EVERY_STATE.slice(0, 3)} />
			<VideoGridEnd />
		</>
	),
});
