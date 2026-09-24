import { expect, fireEvent, fn } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { TAGS } from "@/test/fixtures";
import { useStoryArg } from "@/test/story-args";
import { MultiSelectPicker, type PickerOption } from "./multi-select-picker";

const TAG_OPTIONS: PickerOption<number>[] = TAGS.map((tag) => ({
	id: tag.id,
	label: tag.name,
}));

const GROUP_LABEL = "Tags";
const PLACEHOLDER = "Search tags…";

const removeLabel = (item: string) => i18n.t("common.remove_item", { item });

const meta = preview.meta({
	title: "UI/MultiSelectPicker",
	component: MultiSelectPicker<number>,
	args: {
		"aria-label": GROUP_LABEL,
		options: TAG_OPTIONS,
		selected: [],
		onChange: fn(),
		placeholder: PLACEHOLDER,
		emptyHint: "No tags yet.",
		noMatchesHint: "No tags match your search.",
	},
	decorators: [
		(Story) => (
			<div className="w-80">
				<Story />
			</div>
		),
	],
	render: function Render(args, context) {
		const [selected, setSelected] = useStoryArg(args, "selected", context);
		return (
			<MultiSelectPicker<number>
				{...args}
				selected={selected}
				onChange={(next) => {
					args.onChange(next);
					setSelected(next);
				}}
			/>
		);
	},
});

export const Default = meta.story({
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("group", { name: GROUP_LABEL }),
		).toBeInTheDocument();
		await expect(
			canvas.getByRole("searchbox", { name: PLACEHOLDER }),
		).toBeInTheDocument();
	},
});

export const WithSelection = meta.story({
	args: { selected: [TAGS[1].id, TAGS[4].id, TAGS[6].id] },
	play: async ({ canvas }) => {
		for (const tag of [TAGS[1], TAGS[4], TAGS[6]]) {
			await expect(
				canvas.getByRole("button", { name: removeLabel(tag.name) }),
			).toBeVisible();
			await expect(
				canvas.getByRole("checkbox", { name: tag.name }),
			).toBeChecked();
		}
	},
});

export const SelectOption = meta.story({
	play: async ({ args, canvas, userEvent }) => {
		await userEvent.type(
			canvas.getByRole("searchbox", { name: PLACEHOLDER }),
			TAGS[2].name.slice(0, 4),
		);
		await userEvent.click(canvas.getByRole("checkbox", { name: TAGS[2].name }));
		await expect(args.onChange).toHaveBeenCalledWith([TAGS[2].id]);
		await expect(
			canvas.getByRole("button", { name: removeLabel(TAGS[2].name) }),
		).toBeVisible();
	},
});

export const RemoveChip = meta.story({
	args: { selected: [TAGS[0].id, TAGS[2].id] },
	play: async ({ args, canvas, userEvent }) => {
		await userEvent.click(
			canvas.getByRole("button", { name: removeLabel(TAGS[0].name) }),
		);
		await expect(args.onChange).toHaveBeenCalledWith([TAGS[2].id]);
		await expect(
			canvas.getByRole("checkbox", { name: TAGS[0].name }),
		).not.toBeChecked();
	},
});

export const KeyboardNavigation = meta.story({
	play: async ({ args, canvas, userEvent }) => {
		await userEvent.click(canvas.getByRole("searchbox", { name: PLACEHOLDER }));
		await userEvent.keyboard("{ArrowDown}{ArrowDown}");
		await expect(
			canvas.getByRole("checkbox", { name: TAGS[1].name }),
		).toHaveFocus();
		await userEvent.keyboard(" ");
		await expect(args.onChange).toHaveBeenCalledWith([TAGS[1].id]);
		await userEvent.keyboard("{ArrowUp}{ArrowUp}");
		await expect(
			canvas.getByRole("searchbox", { name: PLACEHOLDER }),
		).toHaveFocus();
	},
});

// Arrow keys belong to the search box and the option list. A chip's remove
// button keeps the browser's default so focus does not jump into the list.
export const ArrowKeysOnChipStayPut = meta.story({
	args: { selected: [TAGS[0].id, TAGS[2].id] },
	play: async ({ canvas }) => {
		const remove = canvas.getByRole("button", {
			name: removeLabel(TAGS[0].name),
		});
		remove.focus();
		await expect(fireEvent.keyDown(remove, { key: "ArrowDown" })).toBe(true);
		await expect(remove).toHaveFocus();
		await expect(fireEvent.keyDown(remove, { key: "ArrowUp" })).toBe(true);
		await expect(remove).toHaveFocus();
	},
});

// A key is only swallowed when it moves focus: the last option has nowhere
// to go, so ArrowDown keeps its default there.
export const ArrowDownPastLastOption = meta.story({
	args: { options: TAG_OPTIONS.slice(0, 2) },
	play: async ({ canvas }) => {
		const last = canvas.getByRole("checkbox", { name: TAG_OPTIONS[1].label });
		last.focus();
		await expect(fireEvent.keyDown(last, { key: "ArrowDown" })).toBe(true);
		await expect(last).toHaveFocus();
		await expect(fireEvent.keyDown(last, { key: "ArrowUp" })).toBe(false);
		await expect(
			canvas.getByRole("checkbox", { name: TAG_OPTIONS[0].label }),
		).toHaveFocus();
	},
});

export const NoOptions = meta.story({
	args: { options: [] },
	play: async ({ canvas }) => {
		await expect(canvas.getByText("No tags yet.")).toBeVisible();
	},
});

export const NoMatches = meta.story({
	play: async ({ canvas, userEvent }) => {
		await userEvent.type(
			canvas.getByRole("searchbox", { name: PLACEHOLDER }),
			"zzz",
		);
		await expect(canvas.getByText("No tags match your search.")).toBeVisible();
	},
});

export const ArrowDownWithoutMatches = meta.story({
	play: async ({ canvas, userEvent }) => {
		const search = canvas.getByRole("searchbox", { name: PLACEHOLDER });
		await userEvent.type(search, "zzz");
		await expect(fireEvent.keyDown(search, { key: "ArrowDown" })).toBe(true);
		await expect(search).toHaveFocus();
		await expect(fireEvent.keyDown(search, { key: "ArrowUp" })).toBe(true);
		await expect(search).toHaveFocus();
	},
});

export const Disabled = meta.story({
	args: { selected: [TAGS[0].id, TAGS[2].id], disabled: true },
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("searchbox", { name: PLACEHOLDER }),
		).toBeDisabled();
		await expect(
			canvas.getByRole("button", { name: removeLabel(TAGS[0].name) }),
		).toBeDisabled();
		await expect(canvas.queryAllByRole("checkbox")).toHaveLength(0);
	},
});
