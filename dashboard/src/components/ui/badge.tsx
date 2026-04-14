import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";

import { cn } from "@/lib/utils";

const badgeVariants = cva(
	"inline-flex items-center gap-1 rounded-md border border-transparent px-2 py-0.5 text-xs font-medium [&_svg]:pointer-events-none [&_svg:not([class*='size-'])]:size-3",
	{
		variants: {
			variant: {
				default: "bg-primary/15 text-foreground",
				secondary: "bg-secondary text-secondary-foreground",
				muted: "bg-muted text-muted-foreground",
				destructive: "bg-destructive/15 text-destructive",
				outline: "border-border bg-transparent text-foreground",
				emerald: "bg-badge-emerald-bg text-badge-emerald-fg",
				blue: "bg-badge-blue-bg text-badge-blue-fg",
				red: "bg-badge-red-bg text-badge-red-fg",
				yellow: "bg-badge-yellow-bg text-badge-yellow-fg",
				green: "bg-badge-green-bg text-badge-green-fg",
				indigo: "bg-badge-indigo-bg text-badge-indigo-fg",
				purple: "bg-badge-purple-bg text-badge-purple-fg",
				pink: "bg-badge-pink-bg text-badge-pink-fg",
				orange: "bg-badge-orange-bg text-badge-orange-fg",
				teal: "bg-badge-teal-bg text-badge-teal-fg",
			},
		},
		defaultVariants: {
			variant: "default",
		},
	},
);

function Badge({
	className,
	variant,
	...props
}: React.ComponentProps<"span"> & VariantProps<typeof badgeVariants>) {
	return (
		<span
			data-slot="badge"
			className={cn(badgeVariants({ variant }), className)}
			{...props}
		/>
	);
}

export { Badge, badgeVariants };
