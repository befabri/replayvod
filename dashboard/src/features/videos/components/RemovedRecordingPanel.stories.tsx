import { expect, fn, waitFor } from "storybook/test";
import preview from "#.storybook/preview";
import type { RestoreInput } from "@/api/generated/trpc";
import i18n from "@/i18n";
import {
	makeVideo,
	VIDEO_REMOVAL_STATES,
	videoHandlers,
} from "@/test/fixtures";
import { mergeTrpcHandlers, trpcParameters } from "@/test/trpc-mock";
import { RemovedRecordingPanel } from "./RemovedRecordingPanel";

const MISSING = makeVideo(0, VIDEO_REMOVAL_STATES.missing);
const REMOVED = makeVideo(1, VIDEO_REMOVAL_STATES.removed);

const restore = fn((_input: RestoreInput) => ({ ok: true }));

const meta = preview.meta({
	title: "Features/Videos/RemovedRecordingPanel",
	component: RemovedRecordingPanel,
	args: { video: MISSING, canManage: true },
	argTypes: { video: { control: false } },
	parameters: trpcParameters(
		mergeTrpcHandlers(videoHandlers([MISSING, REMOVED]), {
			video: { restore },
		}),
	),
});

export const MissingFilesAdmin = meta.story({
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("button", { name: i18n.t("videos.restore") }),
		).toBeVisible();
		await expect(
			canvas.getByRole("button", { name: i18n.t("videos.remove") }),
		).toBeVisible();
	},
});

export const MissingFilesViewer = meta.story({
	args: { canManage: false },
	play: async ({ canvas }) => {
		await expect(
			canvas.queryByRole("button", { name: i18n.t("videos.restore") }),
		).not.toBeInTheDocument();
	},
});

export const RemovedByHand = meta.story({
	args: { video: REMOVED },
	play: async ({ canvas }) => {
		await expect(canvas.getByText(i18n.t("watch.removed"))).toBeVisible();
		await expect(
			canvas.queryByRole("button", { name: i18n.t("videos.restore") }),
		).not.toBeInTheDocument();
	},
});

export const Restore = meta.story({
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(
			canvas.getByRole("button", { name: i18n.t("videos.restore") }),
		);
		await waitFor(() =>
			expect(restore).toHaveBeenCalledWith({ id: MISSING.id }),
		);
	},
});
