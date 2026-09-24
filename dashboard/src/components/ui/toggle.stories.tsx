import { BookmarkSimpleIcon, StarIcon } from "@phosphor-icons/react";
import type { VariantProps } from "class-variance-authority";
import { type ComponentProps, useState } from "react";
import { expect, fn } from "storybook/test";
import preview from "#.storybook/preview";
import { allOf } from "@/test/exhaustive";
import { useStoryArg } from "@/test/story-args";
import { Toggle, type toggleVariants } from "./toggle";

type Variant = NonNullable<VariantProps<typeof toggleVariants>["variant"]>;
type Size = NonNullable<VariantProps<typeof toggleVariants>["size"]>;

const VARIANTS = allOf<Variant>({ ghost: true, outline: true });
const SIZES = allOf<Size>({
	default: true,
	sm: true,
	"icon-sm": true,
	icon: true,
});
const PRESSED_STATES = [false, true];

const FAVORITE = "Favorite";

function FavoriteToggle({
	pressed,
	labelled = false,
	...props
}: ComponentProps<typeof Toggle> & { pressed: boolean; labelled?: boolean }) {
	return (
		<Toggle
			{...props}
			pressed={pressed}
			aria-label={labelled ? undefined : FAVORITE}
		>
			<StarIcon weight={pressed ? "fill" : "regular"} />
			{labelled ? FAVORITE : null}
		</Toggle>
	);
}

function MatrixCell({
	initiallyPressed,
	...props
}: ComponentProps<typeof Toggle> & {
	initiallyPressed: boolean;
	labelled: boolean;
}) {
	const [pressed, setPressed] = useState(initiallyPressed);
	return (
		<FavoriteToggle
			{...props}
			pressed={pressed}
			onPressedChange={(next, details) => {
				props.onPressedChange?.(next, details);
				setPressed(next);
			}}
		/>
	);
}

const meta = preview.meta({
	title: "UI/Toggle",
	component: Toggle,
	args: { pressed: false, onPressedChange: fn() },
	argTypes: {
		variant: { control: "select", options: VARIANTS },
		size: { control: "select", options: SIZES },
	},
	render: function Render(args, context) {
		const [pressed, setPressed] = useStoryArg(args, "pressed", context);
		return (
			<FavoriteToggle
				{...args}
				pressed={pressed ?? false}
				onPressedChange={(next, details) => {
					args.onPressedChange?.(next, details);
					setPressed(next);
				}}
			/>
		);
	},
});

export const Default = meta.story({
	play: async ({ args, canvas, userEvent }) => {
		const toggle = canvas.getByRole("button", { name: FAVORITE });
		await expect(toggle).toHaveAttribute("aria-pressed", "false");
		await userEvent.click(toggle);
		await expect(toggle).toHaveAttribute("aria-pressed", "true");
		await expect(args.onPressedChange).toHaveBeenCalled();
	},
});

export const Pressed = meta.story({
	args: { pressed: true },
});

export const Matrix = meta.story({
	argTypes: {
		pressed: { control: false },
		variant: { control: false },
		size: { control: false },
	},
	render: (args) => (
		<div className="space-y-3">
			{VARIANTS.flatMap((variant) =>
				PRESSED_STATES.map((pressed) => (
					<div
						key={`${variant}-${pressed}`}
						className="flex items-center gap-3"
					>
						{SIZES.map((size) => (
							<MatrixCell
								key={size}
								{...args}
								variant={variant}
								size={size}
								labelled={!size.startsWith("icon")}
								initiallyPressed={pressed}
							/>
						))}
					</div>
				)),
			)}
		</div>
	),
});

export const Disabled = meta.story({
	args: { disabled: true },
	render: (args) => (
		<div className="flex items-center gap-3">
			<FavoriteToggle {...args} pressed={args.pressed ?? false} />
			<Toggle {...args} variant="outline" size="sm">
				<BookmarkSimpleIcon />
				Watch later
			</Toggle>
		</div>
	),
});
