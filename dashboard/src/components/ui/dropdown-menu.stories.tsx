import {
	ArrowSquareOutIcon,
	CopyIcon,
	DotsThreeIcon,
	FunnelSimpleIcon,
	TrashIcon,
} from "@phosphor-icons/react";
import { type ComponentProps, useState } from "react";
import { expect, fn, screen, waitFor } from "storybook/test";
import preview from "#.storybook/preview";
import { CHANNELS, VIDEO_QUALITIES } from "@/test/fixtures";
import { Button } from "./button";
import {
	DropdownMenu,
	DropdownMenuCheckboxItem,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuSubmenu,
	DropdownMenuSubmenuTrigger,
	DropdownMenuTrigger,
} from "./dropdown-menu";

const CHANNEL = CHANNELS[0];

const meta = preview.meta({
	title: "UI/DropdownMenu",
	component: DropdownMenu,
});

export const Default = meta.story({
	args: { onOpenChange: fn() },
	render: (args) => (
		<DropdownMenu {...args}>
			<DropdownMenuTrigger
				render={(triggerProps) => (
					<Button
						variant="outline"
						size="icon"
						aria-label={`${CHANNEL.displayName} actions`}
						{...triggerProps}
					>
						<DotsThreeIcon />
					</Button>
				)}
			/>
			<DropdownMenuContent align="start">
				<DropdownMenuLabel>{CHANNEL.displayName}</DropdownMenuLabel>
				<DropdownMenuSeparator />
				<DropdownMenuItem>
					<ArrowSquareOutIcon className="size-4" />
					Open
				</DropdownMenuItem>
				<DropdownMenuItem>
					<CopyIcon className="size-4" />
					Copy @{CHANNEL.login}
				</DropdownMenuItem>
				<DropdownMenuSubmenu>
					<DropdownMenuSubmenuTrigger>Quality</DropdownMenuSubmenuTrigger>
					<DropdownMenuContent align="start">
						{VIDEO_QUALITIES.map((quality) => (
							<DropdownMenuItem key={quality}>{quality}</DropdownMenuItem>
						))}
					</DropdownMenuContent>
				</DropdownMenuSubmenu>
				<DropdownMenuSeparator />
				<DropdownMenuItem disabled>
					<TrashIcon className="size-4" />
					Disabled item
				</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenu>
	),
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(
			canvas.getByRole("button", { name: `${CHANNEL.displayName} actions` }),
		);
		await waitFor(() => expect(screen.getByRole("menu")).toBeVisible());
		await expect(
			screen.getByRole("menuitem", { name: "Disabled item" }),
		).toHaveAttribute("aria-disabled", "true");
	},
});

function CheckboxMenu(props: ComponentProps<typeof DropdownMenu>) {
	const [visible, setVisible] = useState<Set<string>>(
		() => new Set(CHANNELS.slice(0, 3).map((channel) => channel.id)),
	);
	const toggle = (id: string, checked: boolean) =>
		setVisible((prev) => {
			const next = new Set(prev);
			if (checked) next.add(id);
			else next.delete(id);
			return next;
		});
	return (
		<DropdownMenu {...props}>
			<DropdownMenuTrigger
				render={(triggerProps) => (
					<Button variant="outline" size="sm" {...triggerProps}>
						<FunnelSimpleIcon className="size-4" />
						Channels
					</Button>
				)}
			/>
			<DropdownMenuContent align="start">
				<DropdownMenuLabel>{visible.size} selected</DropdownMenuLabel>
				<DropdownMenuSeparator />
				{CHANNELS.slice(0, 5).map((channel) => (
					<DropdownMenuCheckboxItem
						key={channel.id}
						checked={visible.has(channel.id)}
						onCheckedChange={(checked) => toggle(channel.id, checked)}
						closeOnClick={false}
					>
						{channel.displayName}
					</DropdownMenuCheckboxItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}

export const CheckboxItems = meta.story({
	render: (args) => <CheckboxMenu {...args} />,
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(canvas.getByRole("button", { name: "Channels" }));
		await waitFor(() => expect(screen.getByRole("menu")).toBeVisible());
		await expect(
			screen.getByRole("menuitemcheckbox", { name: CHANNELS[0].displayName }),
		).toHaveAttribute("aria-checked", "true");
		await expect(
			screen.getByRole("menuitemcheckbox", { name: CHANNELS[4].displayName }),
		).toHaveAttribute("aria-checked", "false");
	},
});
