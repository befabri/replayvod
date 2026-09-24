import { expect, fn } from "storybook/test";
import preview from "#.storybook/preview";
import { CATEGORIES, VIDEO_QUALITIES } from "@/test/fixtures";
import { effectiveOpacity } from "@/test/opacity";
import { Label } from "./label";
import { RadioGroup, RadioGroupItem } from "./radio-group";

const OPTIONS = CATEGORIES.slice(0, 3).map((category) => ({
	value: category.id,
	label: category.name,
}));

function Option({
	id,
	value,
	label,
	disabled,
}: {
	id: string;
	value: string;
	label: string;
	disabled?: boolean;
}) {
	return (
		<div className="flex items-center gap-2">
			<RadioGroupItem value={value} id={id} disabled={disabled} />
			<Label htmlFor={id} className="text-sm font-normal">
				{label}
			</Label>
		</div>
	);
}

function Options({ idPrefix }: { idPrefix: string }) {
	return OPTIONS.map((option) => (
		<Option
			key={option.value}
			id={`${idPrefix}-${option.value}`}
			value={option.value}
			label={option.label}
		/>
	));
}

const meta = preview.meta({
	title: "UI/RadioGroup",
	component: RadioGroup,
	args: {
		defaultValue: OPTIONS[0].value,
		onValueChange: fn(),
		className: "flex flex-wrap gap-6",
		"aria-label": "Category",
	},
	render: (args) => (
		<RadioGroup {...args}>
			<Options idPrefix="category" />
		</RadioGroup>
	),
});

export const Default = meta.story();

export const SecondSelected = meta.story({
	args: { defaultValue: OPTIONS[1].value },
});

export const Disabled = meta.story({
	args: { disabled: true },
	render: (args) => (
		<RadioGroup {...args}>
			<Options idPrefix="disabled-category" />
		</RadioGroup>
	),
	play: async ({ canvas }) => {
		for (const radio of canvas.getAllByRole("radio")) {
			await expect(radio).toHaveAttribute("aria-disabled", "true");
			await expect(effectiveOpacity(radio)).toBeLessThan(1);
		}
		for (const option of OPTIONS) {
			await expect(
				effectiveOpacity(canvas.getByText(option.label)),
			).toBeLessThan(1);
		}
	},
});

export const VerticalWithDisabledItem = meta.story({
	args: {
		defaultValue: VIDEO_QUALITIES[0],
		className: undefined,
		"aria-label": "Quality",
	},
	render: (args) => (
		<RadioGroup {...args}>
			{VIDEO_QUALITIES.map((quality, index) => (
				<Option
					key={quality}
					id={`quality-${quality}`}
					value={quality}
					label={quality}
					disabled={index === VIDEO_QUALITIES.length - 1}
				/>
			))}
		</RadioGroup>
	),
	play: async ({ canvas }) => {
		const enabled = canvas.getByRole("radio", { name: VIDEO_QUALITIES[0] });
		const disabled = canvas.getByRole("radio", {
			name: VIDEO_QUALITIES[VIDEO_QUALITIES.length - 1],
		});
		await expect(enabled).not.toHaveAttribute("aria-disabled", "true");
		await expect(disabled).toHaveAttribute("aria-disabled", "true");
		await expect(effectiveOpacity(disabled)).toBeLessThan(
			effectiveOpacity(enabled),
		);
	},
});
