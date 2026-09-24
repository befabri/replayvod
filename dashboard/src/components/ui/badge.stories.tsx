import { BroadcastIcon, TagIcon } from "@phosphor-icons/react";
import type { VariantProps } from "class-variance-authority";
import preview from "#.storybook/preview";
import { allOf } from "@/test/exhaustive";
import { CHANNELS, TAGS } from "@/test/fixtures";
import { Badge, type badgeVariants } from "./badge";

type Variant = NonNullable<VariantProps<typeof badgeVariants>["variant"]>;

const VARIANTS = allOf<Variant>({
	default: true,
	secondary: true,
	muted: true,
	destructive: true,
	outline: true,
	emerald: true,
	blue: true,
	red: true,
	yellow: true,
	green: true,
	indigo: true,
	purple: true,
	pink: true,
	orange: true,
	teal: true,
});

const meta = preview.meta({
	title: "UI/Badge",
	component: Badge,
	args: { children: TAGS[0].name },
	argTypes: {
		variant: { control: "select", options: VARIANTS },
	},
});

export const Default = meta.story();

export const Variants = meta.story({
	render: (args) => (
		<div className="flex flex-wrap items-center gap-2">
			{VARIANTS.map((variant) => (
				<Badge key={variant} {...args} variant={variant}>
					{variant}
				</Badge>
			))}
		</div>
	),
});

export const WithIcon = meta.story({
	render: (args) => (
		<div className="flex flex-wrap items-center gap-2">
			<Badge {...args} variant="red">
				<BroadcastIcon />
				{CHANNELS[0].displayName}
			</Badge>
			{TAGS.slice(1, 4).map((tag) => (
				<Badge key={tag.id} {...args} variant="muted">
					<TagIcon />
					{tag.name}
				</Badge>
			))}
		</div>
	),
});
