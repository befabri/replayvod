import {
	DownloadSimpleIcon,
	GearIcon,
	PlusIcon,
	TrashIcon,
} from "@phosphor-icons/react";
import type { VariantProps } from "class-variance-authority";
import { expect, fn } from "storybook/test";
import preview from "#.storybook/preview";
import { allOf } from "@/test/exhaustive";
import { CHANNELS, VOD_TITLES } from "@/test/fixtures";
import { effectiveOpacity } from "@/test/opacity";
import { Button, type buttonVariants } from "./button";

type Variant = NonNullable<VariantProps<typeof buttonVariants>["variant"]>;
type Size = NonNullable<VariantProps<typeof buttonVariants>["size"]>;

const VARIANTS = allOf<Variant>({
	default: true,
	outline: true,
	secondary: true,
	ghost: true,
	"ghost-muted": true,
	"ghost-destructive": true,
	destructive: true,
	link: true,
	"link-destructive": true,
});
const SIZES = allOf<Size>({
	xs: true,
	sm: true,
	default: true,
	lg: true,
	"icon-xs": true,
	"icon-sm": true,
	icon: true,
	"icon-lg": true,
	inline: true,
});
const TEXT_SIZES = SIZES.filter(
	(size) => !size.startsWith("icon") && size !== "inline",
);
const ICON_SIZES = SIZES.filter((size) => size.startsWith("icon"));

const meta = preview.meta({
	title: "UI/Button",
	component: Button,
	args: { children: "Button", onClick: fn() },
	argTypes: {
		variant: { control: "select", options: VARIANTS },
		size: { control: "select", options: SIZES },
	},
});

export const Default = meta.story();

export const Variants = meta.story({
	render: (args) => (
		<div className="flex flex-wrap items-center gap-2">
			{VARIANTS.map((variant) => (
				<Button key={variant} {...args} variant={variant}>
					{variant}
				</Button>
			))}
		</div>
	),
});

export const Sizes = meta.story({
	render: (args) => (
		<div className="flex flex-col gap-4">
			<div className="flex flex-wrap items-center gap-2">
				{TEXT_SIZES.map((size) => (
					<Button key={size} {...args} size={size}>
						{size}
					</Button>
				))}
			</div>
			<div className="flex flex-wrap items-center gap-2">
				{ICON_SIZES.map((size) => (
					<Button
						key={size}
						{...args}
						size={size}
						variant="outline"
						aria-label="Settings"
					>
						<GearIcon />
					</Button>
				))}
			</div>
		</div>
	),
});

export const WithIcon = meta.story({
	render: (args) => (
		<div className="flex flex-wrap items-center gap-2">
			<Button {...args}>
				<PlusIcon data-icon="inline-start" />
				Add channel
			</Button>
			<Button {...args} variant="outline">
				Download
				<DownloadSimpleIcon data-icon="inline-end" />
			</Button>
			<Button {...args} variant="destructive">
				<TrashIcon data-icon="inline-start" />
				Delete video
			</Button>
		</div>
	),
});

export const Disabled = meta.story({
	args: { disabled: true },
	render: (args) => (
		<div className="flex flex-wrap items-center gap-2">
			{VARIANTS.map((variant) => (
				<Button key={variant} {...args} variant={variant}>
					{variant}
				</Button>
			))}
		</div>
	),
});

export const FocusableWhenDisabled = meta.story({
	args: {
		disabled: true,
		focusableWhenDisabled: true,
		variant: "outline",
		children: "Delete video",
	},
	play: async ({ canvas, userEvent }) => {
		const button = canvas.getByRole("button", { name: "Delete video" });
		await expect(button).toHaveAttribute("aria-disabled", "true");
		await userEvent.tab();
		await expect(button).toHaveFocus();
		await expect(effectiveOpacity(button)).toBe(0.5);
	},
});

export const InlineLinkWraps = meta.story({
	render: (args) => (
		<p className="w-40 text-sm">
			<Button {...args} variant="link" size="inline">
				{VOD_TITLES[0]}
			</Button>
		</p>
	),
	play: async ({ canvas }) => {
		const link = canvas.getByRole("button");
		await expect(link.getBoundingClientRect().height).toBeGreaterThan(24);
		await expect(getComputedStyle(link).userSelect).not.toBe("none");
	},
});

export const InlineTextActions = meta.story({
	render: (args) => (
		<ul className="w-96 divide-y divide-border rounded-lg bg-card px-4 text-sm">
			{CHANNELS.slice(0, 3).map((channel) => (
				<li key={channel.id} className="flex items-center gap-3 py-3">
					<span className="min-w-0 flex-1 truncate">{channel.displayName}</span>
					<Button {...args} variant="link" size="inline">
						Edit
					</Button>
					<Button {...args} variant="link-destructive" size="inline">
						Remove
					</Button>
				</li>
			))}
		</ul>
	),
});
