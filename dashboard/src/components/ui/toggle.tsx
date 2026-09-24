import { Toggle as TogglePrimitive } from "@base-ui/react/toggle";
import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";

import { cn } from "@/lib/utils";

const toggleVariants = cva(
	"inline-flex shrink-0 items-center justify-center gap-1.5 rounded-md border border-transparent text-sm font-medium whitespace-nowrap text-muted-foreground transition-colors duration-75 outline-none select-none hover:text-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:pointer-events-none not-in-data-dimmed:disabled:opacity-50 data-[pressed]:text-primary data-[pressed]:hover:text-primary [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
	{
		variants: {
			variant: {
				ghost: "hover:bg-accent",
				outline: "border-border bg-input/30 hover:bg-input/50",
			},
			size: {
				default: "h-9 px-3",
				sm: "h-8 px-3",
				"icon-sm": "size-8",
				icon: "size-9",
			},
		},
		defaultVariants: {
			variant: "ghost",
			size: "icon-sm",
		},
	},
);

function Toggle({
	className,
	variant,
	size,
	...props
}: React.ComponentProps<typeof TogglePrimitive> &
	VariantProps<typeof toggleVariants>) {
	return (
		<TogglePrimitive
			data-slot="toggle"
			className={cn(toggleVariants({ variant, size }), className)}
			{...props}
		/>
	);
}

export { Toggle, toggleVariants };
