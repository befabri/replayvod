import { TrashIcon } from "@phosphor-icons/react";
import type { StoryContext } from "@storybook/react-vite";
import type { ComponentProps } from "react";
import { expect, fn, screen, waitFor, within } from "storybook/test";
import preview from "#.storybook/preview";
import { CHANNELS, VOD_TITLES } from "@/test/fixtures";
import { useStoryArg } from "@/test/story-args";
import { Button } from "./button";
import { ConfirmDialog } from "./confirm-dialog";

type ConfirmDialogProps = ComponentProps<typeof ConfirmDialog>;

function withTrigger(triggerLabel: string) {
	return function Render(
		args: ConfirmDialogProps,
		context: Pick<StoryContext, "id">,
	) {
		const [open, setOpen] = useStoryArg(args, "open", context);
		return (
			<>
				<Button
					variant={args.destructive ? "destructive" : "outline"}
					onClick={() => setOpen(true)}
				>
					{args.destructive ? <TrashIcon data-icon="inline-start" /> : null}
					{triggerLabel}
				</Button>
				<ConfirmDialog
					{...args}
					open={open}
					onOpenChange={(next) => {
						args.onOpenChange(next);
						setOpen(next);
					}}
				/>
			</>
		);
	};
}

async function openDialog(
	{ canvas, userEvent }: Pick<StoryContext, "canvas" | "userEvent">,
	triggerLabel: string,
) {
	await userEvent.click(canvas.getByRole("button", { name: triggerLabel }));
	await waitFor(() => expect(screen.getByRole("dialog")).toBeVisible());
	return screen.getByRole("dialog");
}

const meta = preview.meta({
	title: "UI/ConfirmDialog",
	component: ConfirmDialog,
	args: {
		open: false,
		onOpenChange: fn(),
		onConfirm: fn(),
		title: `Follow ${CHANNELS[1].displayName}?`,
		description: `@${CHANNELS[1].login}`,
		confirmLabel: "Confirm",
		cancelLabel: "Cancel",
	},
	render: withTrigger("Open"),
});

export const Default = meta.story({
	play: async ({ args, canvas, userEvent }) => {
		const dialog = await openDialog({ canvas, userEvent }, "Open");
		await userEvent.click(
			within(dialog).getByRole("button", { name: args.confirmLabel }),
		);
		await expect(args.onConfirm).toHaveBeenCalledOnce();
	},
});

export const Destructive = meta.story({
	args: {
		destructive: true,
		title: `Remove “${VOD_TITLES[0]}”?`,
		description: CHANNELS[0].displayName,
		confirmLabel: "Remove",
	},
	render: withTrigger("Remove"),
	play: async ({ canvas, userEvent }) => {
		const dialog = await openDialog({ canvas, userEvent }, "Remove");
		for (const button of within(dialog).getAllByRole("button")) {
			await expect(button).toBeEnabled();
		}
	},
});

export const Confirming = meta.story({
	args: {
		destructive: true,
		confirming: true,
		title: `Remove “${VOD_TITLES[1]}”?`,
		description: CHANNELS[1].displayName,
		confirmLabel: "Removing…",
	},
	render: withTrigger("Remove"),
	play: async ({ args, canvas, userEvent }) => {
		const dialog = await openDialog({ canvas, userEvent }, "Remove");
		await expect(
			within(dialog).getByRole("button", { name: args.confirmLabel }),
		).toBeDisabled();
		await expect(
			within(dialog).getByRole("button", { name: args.cancelLabel }),
		).toBeDisabled();
	},
});

export const WithoutDescription = meta.story({
	args: {
		description: undefined,
		cancelLabel: "Not now",
	},
	play: async ({ args, canvas, userEvent }) => {
		const dialog = await openDialog({ canvas, userEvent }, "Open");
		await userEvent.click(
			within(dialog).getByRole("button", { name: args.cancelLabel }),
		);
		await expect(args.onOpenChange).toHaveBeenCalledWith(false);
		await waitFor(() =>
			expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
		);
	},
});
