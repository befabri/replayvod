import { expect, fn, screen, waitFor } from "storybook/test";
import preview from "#.storybook/preview";
import type { VideoDeleteInput } from "@/api/generated/trpc";
import i18n from "@/i18n";
import { makeVideo, VIDEO_STATES, videoHandlers } from "@/test/fixtures";
import { useStoryArg } from "@/test/story-args";
import { mergeTrpcHandlers, trpcParameters } from "@/test/trpc-mock";
import { WatchHeaderActions } from "./WatchHeaderActions";

const VIDEO = makeVideo(0);

const deleteVideo = fn((_input: VideoDeleteInput) => ({ ok: true }));

const meta = preview.meta({
	title: "Features/Videos/WatchHeaderActions",
	component: WatchHeaderActions,
	args: {
		video: VIDEO,
		canManage: false,
		layout: "aside" as const,
		onLayoutChange: fn(),
		onRemoved: fn(),
	},
	argTypes: { video: { control: false } },
	globals: { viewport: { value: "desktop", isRotated: false } },
	parameters: trpcParameters(
		mergeTrpcHandlers(videoHandlers([VIDEO]), {
			video: { delete: deleteVideo },
		}),
	),
	render: function Render(args, context) {
		const [layout, setLayout] = useStoryArg(args, "layout", context);
		return (
			<WatchHeaderActions
				{...args}
				layout={layout}
				onLayoutChange={(next) => {
					args.onLayoutChange(next);
					setLayout(next);
				}}
			/>
		);
	},
});

export const Viewer = meta.story({
	play: async ({ canvas }) => {
		const watchLater = canvas.getByRole("button", {
			name: i18n.t("videos.watch_later.label"),
		});
		await expect(watchLater).toHaveAttribute("aria-pressed", "false");
		await expect(watchLater).toHaveAttribute(
			"title",
			i18n.t("videos.watch_later.add"),
		);
		await expect(
			canvas.queryByRole("button", { name: i18n.t("videos.remove") }),
		).not.toBeInTheDocument();
	},
});

export const Admin = meta.story({
	args: { canManage: true },
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("button", { name: i18n.t("videos.remove") }),
		).toBeVisible();
	},
});

export const InWatchLater = meta.story({
	args: { video: makeVideo(0, VIDEO_STATES.watchLater) },
	play: async ({ canvas }) => {
		const watchLater = canvas.getByRole("button", {
			name: i18n.t("videos.watch_later.label"),
		});
		await expect(watchLater).toHaveAttribute("aria-pressed", "true");
		await expect(watchLater).toHaveAttribute(
			"title",
			i18n.t("videos.watch_later.remove"),
		);
	},
});

export const SwitchLayout = meta.story({
	play: async ({ args, canvas, userEvent }) => {
		await userEvent.click(
			canvas.getByRole("button", { name: i18n.t("watch.switch_to_wide") }),
		);
		await expect(args.onLayoutChange).toHaveBeenCalledWith("wide");
		await expect(
			canvas.getByRole("button", { name: i18n.t("watch.switch_to_aside") }),
		).toBeVisible();
	},
});

export const RemoveAfterConfirm = meta.story({
	args: { canManage: true },
	play: async ({ args, canvas, userEvent }) => {
		await userEvent.click(
			canvas.getByRole("button", { name: i18n.t("videos.remove") }),
		);
		await waitFor(() => expect(screen.getByRole("dialog")).toBeVisible());
		await userEvent.click(
			screen.getByRole("button", { name: i18n.t("videos.remove_confirm") }),
		);
		await waitFor(() =>
			expect(deleteVideo).toHaveBeenCalledWith({ id: VIDEO.id }),
		);
		await waitFor(() => expect(args.onRemoved).toHaveBeenCalled());
	},
});
