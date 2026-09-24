import { type ReactNode, useState } from "react";
import { expect, screen, waitFor } from "storybook/test";
import preview from "#.storybook/preview";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatDuration } from "@/features/videos/format";
import {
	CATEGORIES,
	CHANNELS,
	type FakeCategory,
	type FakeChannel,
	makeVideos,
} from "@/test/fixtures";
import { effectiveOpacity } from "@/test/opacity";
import {
	Combobox,
	ComboboxChip,
	ComboboxChipRemove,
	ComboboxChips,
	ComboboxChipsInput,
	ComboboxCollection,
	ComboboxContent,
	ComboboxEmpty,
	ComboboxGroup,
	ComboboxGroupLabel,
	ComboboxInput,
	ComboboxItem,
	ComboboxList,
	ComboboxStatus,
	ComboboxTrigger,
} from "./combobox";

type SearchResult = { id: string; label: string; detail: string };
type SearchGroup = { value: string; items: SearchResult[] };

const SEARCH_GROUPS: SearchGroup[] = [
	{
		value: "Channels",
		items: CHANNELS.slice(0, 2).map((channel, index) => ({
			id: `channel-${channel.id}`,
			label: channel.displayName,
			detail: `${37 - index * 25} videos`,
		})),
	},
	{
		value: "Videos",
		items: makeVideos(3).map((video) => ({
			id: `video-${video.id}`,
			label: video.title,
			detail: `${video.broadcaster_name} · ${formatDuration(video.duration_seconds)} · ${video.quality}`,
		})),
	},
];

const NO_CHANNELS = "No channels found";
const CLEAR_CATEGORIES = "Clear categories";
const LOADING = "Loading…";
const PRESELECTED_CATEGORIES = [CATEGORIES[0], CATEGORIES[3]];
const [UNSELECTED_CATEGORY] = CATEGORIES.filter(
	(category) => !PRESELECTED_CATEGORIES.includes(category),
);

const meta = preview.meta({
	title: "UI/Combobox",
	component: Combobox,
});

function ChannelCombobox({ initial = null }: { initial?: FakeChannel | null }) {
	const [value, setValue] = useState<FakeChannel | null>(initial);
	return (
		<div className="w-80">
			<Combobox<FakeChannel>
				items={CHANNELS}
				value={value}
				onValueChange={setValue}
				itemToStringLabel={(channel) => channel.displayName}
				itemToStringValue={(channel) => channel.id}
				isItemEqualToValue={(a, b) => a.id === b.id}
			>
				<div className="relative">
					<ComboboxInput
						aria-label="Channel"
						placeholder="Search channels…"
						className="pr-9"
					/>
					<ComboboxTrigger
						aria-label="Show channels"
						className="absolute right-1.5 top-1/2 -translate-y-1/2"
					/>
				</div>
				<ComboboxContent>
					<ComboboxList<FakeChannel>>
						{(item) => (
							<ComboboxItem key={item.id} value={item}>
								<Avatar
									src={item.profileImageUrl}
									name={item.displayName}
									size="sm"
								/>
								<div className="flex min-w-0 flex-1 flex-col">
									<span className="truncate font-medium">
										{item.displayName}
									</span>
									<span className="truncate font-mono text-xs text-muted-foreground">
										{item.login}
									</span>
								</div>
							</ComboboxItem>
						)}
					</ComboboxList>
					<ComboboxEmpty>{NO_CHANNELS}</ComboboxEmpty>
				</ComboboxContent>
			</Combobox>
		</div>
	);
}

export const Default = meta.story({
	render: () => <ChannelCombobox />,
	play: async ({ canvas, userEvent }) => {
		await userEvent.type(
			canvas.getByRole("combobox"),
			CHANNELS[0].displayName.slice(0, 3),
		);
		await waitFor(() => expect(screen.getByRole("listbox")).toBeVisible());
		await expect(
			screen.getByRole("option", {
				name: new RegExp(CHANNELS[0].displayName),
			}),
		).toBeInTheDocument();
	},
});

export const WithValue = meta.story({
	render: () => <ChannelCombobox initial={CHANNELS[0]} />,
});

export const NoMatches = meta.story({
	render: () => <ChannelCombobox />,
	play: async ({ canvas, userEvent }) => {
		await userEvent.type(canvas.getByRole("combobox"), "zzz");
		await waitFor(() => expect(screen.getByText(NO_CHANNELS)).toBeVisible());
	},
});

function CategoryCombobox({
	disabled,
	children,
}: {
	disabled?: boolean;
	children?: ReactNode;
}) {
	const [selected, setSelected] = useState<FakeCategory[]>(
		PRESELECTED_CATEGORIES,
	);
	return (
		<div className="w-96">
			<Combobox<FakeCategory, true>
				multiple
				items={CATEGORIES}
				value={selected}
				onValueChange={setSelected}
				itemToStringLabel={(category) => category.name}
				itemToStringValue={(category) => category.id}
				isItemEqualToValue={(a, b) => a.id === b.id}
				disabled={disabled}
			>
				<ComboboxChips>
					{selected.map((category) => (
						<ComboboxChip key={category.id}>
							{category.name}
							<ComboboxChipRemove />
						</ComboboxChip>
					))}
					<ComboboxChipsInput
						aria-label="Categories"
						placeholder="Search categories…"
					/>
					{children}
				</ComboboxChips>
				<ComboboxContent>
					<ComboboxList<FakeCategory>>
						{(item) => (
							<ComboboxItem key={item.id} value={item}>
								<span className="truncate">{item.name}</span>
							</ComboboxItem>
						)}
					</ComboboxList>
					<ComboboxEmpty>No categories match</ComboboxEmpty>
				</ComboboxContent>
			</Combobox>
		</div>
	);
}

export const MultipleWithChips = meta.story({
	render: () => <CategoryCombobox />,
	play: async ({ canvas, userEvent }) => {
		await expect(effectiveOpacity(canvas.getByRole("combobox"))).toBe(1);
		await userEvent.type(
			canvas.getByRole("combobox"),
			UNSELECTED_CATEGORY.name.slice(0, 4),
		);
		await waitFor(() => expect(screen.getByRole("listbox")).toBeVisible());
		await expect(
			screen.getByRole("option", { name: UNSELECTED_CATEGORY.name }),
		).toBeInTheDocument();
	},
});

// A disabled chips field dims once, on the container. The input and any
// control placed among the chips skip their own dimming instead of stacking
// a second 50% on top.
export const DisabledChips = meta.story({
	render: () => (
		<CategoryCombobox disabled>
			<Button
				variant="ghost"
				size="icon-sm"
				disabled
				aria-label={CLEAR_CATEGORIES}
			>
				×
			</Button>
		</CategoryCombobox>
	),
	play: async ({ canvas }) => {
		const input = canvas.getByRole("combobox");
		await expect(input).toBeDisabled();
		await expect(input.closest('[data-slot="combobox-chips"]')).toHaveAttribute(
			"data-dimmed",
		);
		await expect(effectiveOpacity(input)).toBe(0.5);
		await expect(
			effectiveOpacity(canvas.getByRole("button", { name: CLEAR_CATEGORIES })),
		).toBe(0.5);
	},
});

export const Loading = meta.story({
	render: () => (
		<div className="w-80">
			<Combobox<FakeChannel> items={[]} filter={null}>
				<ComboboxInput aria-label="Channel" placeholder="Search channels…" />
				<ComboboxContent>
					<ComboboxList<FakeChannel>>
						{(item) => (
							<ComboboxItem key={item.id} value={item}>
								{item.displayName}
							</ComboboxItem>
						)}
					</ComboboxList>
					<ComboboxStatus>{LOADING}</ComboboxStatus>
				</ComboboxContent>
			</Combobox>
		</div>
	),
	play: async ({ canvas, userEvent }) => {
		await userEvent.type(
			canvas.getByRole("combobox"),
			CHANNELS[0].displayName.slice(0, 3),
		);
		await waitFor(() => expect(screen.getByText(LOADING)).toBeVisible());
	},
});

export const Grouped = meta.story({
	render: () => (
		<div className="w-96">
			<Combobox<SearchResult>
				items={SEARCH_GROUPS}
				itemToStringLabel={(result) => result.label}
				itemToStringValue={(result) => result.id}
			>
				<ComboboxInput
					aria-label="Search"
					placeholder="Search videos, channels, categories…"
				/>
				<ComboboxContent className="w-[var(--anchor-width)]">
					<ComboboxList<SearchGroup>>
						{(group) => (
							<ComboboxGroup key={group.value} items={group.items}>
								<ComboboxGroupLabel>
									<span>{group.value}</span>
									<Badge variant="muted" className="tabular-nums">
										{group.items.length}
									</Badge>
								</ComboboxGroupLabel>
								<ComboboxCollection<SearchResult>>
									{(result) => (
										<ComboboxItem key={result.id} value={result}>
											<div className="flex min-w-0 flex-1 flex-col">
												<span className="truncate font-medium">
													{result.label}
												</span>
												<span className="truncate text-xs opacity-70">
													{result.detail}
												</span>
											</div>
										</ComboboxItem>
									)}
								</ComboboxCollection>
							</ComboboxGroup>
						)}
					</ComboboxList>
					<ComboboxEmpty>No results</ComboboxEmpty>
				</ComboboxContent>
			</Combobox>
		</div>
	),
	play: async ({ canvas, userEvent }) => {
		await userEvent.type(
			canvas.getByRole("combobox"),
			SEARCH_GROUPS[0].items[0].label.slice(0, 3),
		);
		await waitFor(() => expect(screen.getByRole("listbox")).toBeVisible());
		await waitFor(() =>
			expect(screen.getByText(SEARCH_GROUPS[0].value)).toBeVisible(),
		);
	},
});
