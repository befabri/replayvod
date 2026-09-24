import { Button as ButtonPrimitive } from "@base-ui/react/button";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const BLOCK_SIZE = "whitespace-nowrap select-none";

const buttonVariants = cva(
	"group/button inline-flex shrink-0 items-center justify-center rounded-md border border-transparent bg-clip-padding text-sm font-medium transition-colors duration-75 outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 active:not-aria-[haspopup]:translate-y-px disabled:pointer-events-none not-in-data-dimmed:disabled:opacity-50 aria-disabled:cursor-not-allowed not-in-data-dimmed:aria-disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-[3px] aria-invalid:ring-destructive/20 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
	{
		variants: {
			variant: {
				default: "bg-primary text-primary-foreground hover:bg-primary-hover",
				outline:
					"border-border bg-input/30 hover:bg-input/50 hover:text-foreground aria-expanded:bg-muted aria-expanded:text-foreground",
				secondary:
					"bg-secondary text-secondary-foreground hover:bg-secondary/80 aria-expanded:bg-secondary aria-expanded:text-secondary-foreground",
				ghost:
					"hover:bg-accent hover:text-accent-foreground aria-expanded:bg-accent aria-expanded:text-accent-foreground",
				"ghost-muted":
					"text-muted-foreground hover:bg-accent hover:text-foreground aria-expanded:bg-accent aria-expanded:text-foreground",
				"ghost-destructive":
					"text-muted-foreground hover:bg-destructive/15 hover:text-destructive focus-visible:border-destructive/40 focus-visible:ring-destructive/20",
				destructive:
					"bg-destructive/15 text-destructive hover:bg-destructive/25 focus-visible:border-destructive/40 focus-visible:ring-destructive/20",
				link: "text-link underline-offset-4 hover:underline",
				"link-destructive":
					"text-destructive underline-offset-4 hover:underline",
			},
			size: {
				default: [
					BLOCK_SIZE,
					"h-9 gap-1.5 px-3 has-data-[icon=inline-end]:pr-2.5 has-data-[icon=inline-start]:pl-2.5",
				],
				xs: [
					BLOCK_SIZE,
					"h-6 gap-1 px-2.5 text-xs has-data-[icon=inline-end]:pr-2 has-data-[icon=inline-start]:pl-2 [&_svg:not([class*='size-'])]:size-3",
				],
				sm: [
					BLOCK_SIZE,
					"h-8 gap-1 px-3 has-data-[icon=inline-end]:pr-2 has-data-[icon=inline-start]:pl-2",
				],
				lg: [
					BLOCK_SIZE,
					"h-10 gap-1.5 px-4 has-data-[icon=inline-end]:pr-3 has-data-[icon=inline-start]:pl-3",
				],
				icon: [BLOCK_SIZE, "size-9"],
				"icon-xs": [BLOCK_SIZE, "size-6 [&_svg:not([class*='size-'])]:size-3"],
				"icon-sm": [BLOCK_SIZE, "size-8"],
				"icon-lg": [BLOCK_SIZE, "size-10"],
				inline: "h-auto gap-1 p-0 text-left",
			},
		},
		defaultVariants: {
			variant: "default",
			size: "default",
		},
	},
);

function Button({
	className,
	variant = "default",
	size = "default",
	...props
}: ButtonPrimitive.Props & VariantProps<typeof buttonVariants>) {
	return (
		<ButtonPrimitive
			data-slot="button"
			className={cn(buttonVariants({ variant, size, className }))}
			{...props}
		/>
	);
}

export { Button, buttonVariants };
