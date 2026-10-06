import { expect, screen } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { makeTimeline, makeVideo } from "@/test/fixtures";
import { trpcParameters } from "@/test/trpc-mock";
import { StreamHistoryButton } from "./StreamHistoryButton";

const VIDEO = makeVideo(0);
const LONG_TITLE =
	"Exploring every corner of the new expansion with viewers, trying the optional challenges and taking the scenic route before the final boss";

const meta = preview.meta({
	title: "Features/Videos/StreamHistoryButton",
	component: StreamHistoryButton,
	args: {
		videoId: VIDEO.id,
		videoStartDownloadAt: VIDEO.start_download_at,
		t: i18n.t,
	},
	parameters: trpcParameters({
		video: {
			timeline: () =>
				makeTimeline(VIDEO).map((event) =>
					event.title && !event.category
						? { ...event, title: { ...event.title, name: LONG_TITLE } }
						: event,
				),
		},
	}),
});

export const Timeline = meta.story({
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(
			canvas.getByRole("button", { name: i18n.t("videos.history.tooltip") }),
		);
		const title = await screen.findByText(LONG_TITLE);
		const timestamp = title.closest("li")?.querySelector("a");
		if (!timestamp) throw new Error("Timeline timestamp is missing");
		const range = document.createRange();
		range.selectNodeContents(title);
		const firstLine = range.getClientRects()[0];
		await expect(title.getBoundingClientRect().height).toBeGreaterThan(
			firstLine.height * 2,
		);
		const chip = timestamp.getBoundingClientRect();
		await expect(
			Math.abs(
				chip.top + chip.height / 2 - firstLine.top - firstLine.height / 2,
			),
		).toBeLessThan(4);
	},
});
