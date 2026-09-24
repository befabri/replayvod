import { InfoIcon, PencilSimpleIcon } from "@phosphor-icons/react";
import { expect, screen, waitFor } from "storybook/test";
import preview from "#.storybook/preview";
import { CHANNELS, VOD_TITLES } from "@/test/fixtures";
import { Button } from "./button";
import {
	Dialog,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "./dialog";
import { Input } from "./input";
import { Label } from "./label";

const CHANNEL = CHANNELS[0];

const meta = preview.meta({
	title: "UI/Dialog",
	component: Dialog,
});

export const Default = meta.story({
	render: (args) => (
		<Dialog {...args}>
			<DialogTrigger
				render={
					<Button>
						<PencilSimpleIcon data-icon="inline-start" />
						Edit title
					</Button>
				}
			/>
			<DialogContent className="max-w-md">
				<DialogHeader>
					<DialogTitle>Edit title</DialogTitle>
					<DialogDescription>{CHANNEL.displayName}</DialogDescription>
				</DialogHeader>
				<form
					className="flex flex-col gap-3"
					onSubmit={(event) => event.preventDefault()}
				>
					<div className="flex flex-col gap-1.5">
						<Label htmlFor="story-title">Title</Label>
						<Input id="story-title" defaultValue={VOD_TITLES[0]} />
					</div>
					<DialogFooter>
						<DialogClose render={<Button variant="outline">Cancel</Button>} />
						<Button type="submit">Save</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	),
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(canvas.getByRole("button", { name: "Edit title" }));
		await waitFor(() => expect(screen.getByRole("dialog")).toBeVisible());
		await expect(screen.getByLabelText("Title")).toHaveValue(VOD_TITLES[0]);
	},
});

export const WithoutCloseButton = meta.story({
	render: (args) => (
		<Dialog {...args}>
			<DialogTrigger
				render={<Button variant="outline">{CHANNEL.displayName}</Button>}
			/>
			<DialogContent className="max-w-sm" showCloseButton={false}>
				<DialogHeader>
					<DialogTitle>{CHANNEL.displayName}</DialogTitle>
					<DialogDescription>@{CHANNEL.login}</DialogDescription>
				</DialogHeader>
				<DialogFooter>
					<DialogClose render={<Button>Close</Button>} />
				</DialogFooter>
			</DialogContent>
		</Dialog>
	),
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(
			canvas.getByRole("button", { name: CHANNEL.displayName }),
		);
		await waitFor(() => expect(screen.getByRole("dialog")).toBeVisible());
	},
});

export const LongDescription = meta.story({
	render: (args) => (
		<Dialog {...args}>
			<DialogTrigger
				render={
					<Button variant="ghost" size="icon" aria-label="Details">
						<InfoIcon />
					</Button>
				}
			/>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>Details</DialogTitle>
					<DialogDescription>{VOD_TITLES.join(". ")}.</DialogDescription>
				</DialogHeader>
			</DialogContent>
		</Dialog>
	),
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(canvas.getByRole("button", { name: "Details" }));
		await waitFor(() => expect(screen.getByRole("dialog")).toBeVisible());
	},
});
