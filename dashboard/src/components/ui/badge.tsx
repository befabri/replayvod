import { cva, type VariantProps } from "class-variance-authority"
import type * as React from "react"

import { cn } from "@/lib/utils"

const badgeVariants = cva(
	"inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-medium [&_svg]:pointer-events-none [&_svg:not([class*='size-'])]:size-3",
	{
		variants: {
			variant: {
				default: "border-transparent bg-primary/15 text-primary-foreground",
				secondary:
					"border-transparent bg-secondary text-secondary-foreground",
				muted: "border-transparent bg-muted text-muted-foreground",
				success: "border-transparent bg-primary/20 text-primary-foreground",
				warning:
					"border-transparent bg-yellow-500/20 text-yellow-100 dark:text-yellow-200",
				destructive:
					"border-transparent bg-destructive/15 text-destructive",
				outline: "border-border text-foreground",
			},
		},
		defaultVariants: {
			variant: "default",
		},
	},
)

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
	)
}

export { Badge, badgeVariants }
