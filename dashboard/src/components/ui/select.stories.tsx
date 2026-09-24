import { FunnelSimpleIcon } from "@phosphor-icons/react";
import { type ComponentProps, useState } from "react";
import { expect, screen, waitFor } from "storybook/test";
import preview from "#.storybook/preview";
import { allOf } from "@/test/exhaustive";
import { CATEGORIES, CHANNELS, VIDEO_QUALITIES } from "@/test/fixtures";
import { Label } from "./label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "./select";

type TriggerVariant = NonNullable<
	ComponentProps<typeof SelectTrigger>["variant"]
>;

const TRIGGER_VARIANTS = allOf<TriggerVariant>({ default: true, chip: true });

const CHANNEL_ITEMS = CHANNELS.map((channel) => ({
	value: channel.id,
	label: channel.displayName,
}));

const meta = preview.meta({
	title: "UI/Select",
	component: Select,
});

function ChannelSelect({
	disabled,
	invalid,
	initial = null,
}: {
	disabled?: boolean;
	invalid?: boolean;
	initial?: string | null;
}) {
	const [value, setValue] = useState<string | null>(initial);
	return (
		<div className="flex w-72 flex-col gap-1.5">
			<Label htmlFor="story-channel">Channel</Label>
			<Select
				value={value}
				onValueChange={(next) => setValue(next as string)}
				disabled={disabled}
				items={CHANNEL_ITEMS}
			>
				<SelectTrigger id="story-channel" aria-invalid={invalid}>
					<SelectValue placeholder="Choose a channel" />
				</SelectTrigger>
				<SelectContent>
					{CHANNEL_ITEMS.map((item) => (
						<SelectItem key={item.value} value={item.value}>
							{item.label}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
		</div>
	);
}

export const Default = meta.story({
	render: () => <ChannelSelect />,
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(canvas.getByRole("combobox"));
		await waitFor(() =>
			expect(screen.getByRole("listbox", { name: "Channel" })).toBeVisible(),
		);
		await waitFor(() =>
			expect(
				screen.getByRole("option", { name: CHANNEL_ITEMS[2].label }),
			).toBeVisible(),
		);
	},
});

export const WithValue = meta.story({
	render: () => <ChannelSelect initial={CHANNEL_ITEMS[1].value} />,
});

export const Disabled = meta.story({
	render: () => <ChannelSelect initial={CHANNEL_ITEMS[0].value} disabled />,
});

export const Invalid = meta.story({
	render: () => <ChannelSelect invalid />,
});

function QualityChipSelect({ variant }: { variant: TriggerVariant }) {
	const [current, setCurrent] = useState<string>(VIDEO_QUALITIES[0]);
	return (
		<Select value={current} onValueChange={(next) => setCurrent(String(next))}>
			<SelectTrigger
				variant={variant}
				className="min-w-[150px]"
				aria-label="Quality"
			>
				<div className="flex items-center gap-2">
					<FunnelSimpleIcon className="size-4 text-muted-foreground" />
					<span className="truncate text-sm font-medium">{current}</span>
				</div>
			</SelectTrigger>
			<SelectContent>
				{VIDEO_QUALITIES.map((quality) => (
					<SelectItem key={quality} value={quality}>
						{quality}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
	);
}

export const TriggerVariants = meta.story({
	render: () => (
		<div className="flex flex-wrap items-center gap-3">
			{TRIGGER_VARIANTS.map((variant) => (
				<QualityChipSelect key={variant} variant={variant} />
			))}
		</div>
	),
});

export const Chip = meta.story({
	render: () => <QualityChipSelect variant="chip" />,
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(canvas.getByRole("combobox"));
		await waitFor(() => expect(screen.getByRole("listbox")).toBeVisible());
		await expect(
			screen.getByRole("option", { name: VIDEO_QUALITIES[0] }),
		).toHaveAttribute("aria-selected", "true");
	},
});

export const RenderedValue = meta.story({
	render: (args) => (
		<Select defaultValue={CATEGORIES[0].id} {...args}>
			<SelectTrigger aria-label="Category" className="w-48">
				<SelectValue>
					{(value: unknown) =>
						CATEGORIES.find((category) => category.id === value)?.name
					}
				</SelectValue>
			</SelectTrigger>
			<SelectContent>
				{CATEGORIES.slice(0, 4).map((category) => (
					<SelectItem key={category.id} value={category.id}>
						{category.name}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
	),
});
