import { expect, fn } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { useStoryArg } from "@/test/story-args";
import { HistoryViewControls } from "./HistoryViewControls";

const meta = preview.meta({
	title: "Features/Videos/HistoryViewControls",
	component: HistoryViewControls,
	args: {
		view: { outcome: "all", media: "any" } as const,
		counts: { all: 214, failed: 9, cancelled: 3 },
		onViewChange: fn(),
	},
	render: function Render(args, context) {
		const [view, setView] = useStoryArg(args, "view", context);
		return (
			<HistoryViewControls
				{...args}
				view={view}
				onViewChange={(next) => {
					args.onViewChange(next);
					setView(next);
				}}
			/>
		);
	},
	parameters: { layout: "padded" },
});

export const Default = meta.story();

export const FailedRemoved = meta.story({
	args: { view: { outcome: "failed", media: "removed" } },
});

export const CountsLoading = meta.story({
	args: {
		counts: { all: undefined, failed: undefined, cancelled: undefined },
	},
});

export const MediaCannotBeCleared = meta.story({
	play: async ({ args, canvas, userEvent }) => {
		const any = canvas.getByRole("button", {
			name: i18n.t("history.scope_any"),
		});
		await userEvent.click(any);
		await expect(args.onViewChange).not.toHaveBeenCalled();
		await expect(any).toHaveAttribute("aria-pressed", "true");
	},
});

export const SelectMedia = meta.story({
	play: async ({ args, canvas, userEvent }) => {
		await userEvent.click(
			canvas.getByRole("button", { name: i18n.t("history.scope_on_disk") }),
		);
		await expect(args.onViewChange).toHaveBeenCalledWith({
			outcome: "all",
			media: "on_disk",
		});
	},
});
