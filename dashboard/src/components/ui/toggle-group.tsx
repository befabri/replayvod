import { Toggle as TogglePrimitive } from "@base-ui/react/toggle";
import { ToggleGroup as ToggleGroupPrimitive } from "@base-ui/react/toggle-group";
import type * as React from "react";

import { cn } from "@/lib/utils";

function ToggleGroup({
	className,
	...props
}: React.ComponentProps<typeof ToggleGroupPrimitive>) {
	return (
		<ToggleGroupPrimitive
			data-slot="toggle-group"
			className={cn(
				"inline-flex items-center rounded-md border border-border bg-card p-0.5",
				className,
			)}
			{...props}
		/>
	);
}

function ToggleGroupItem({
	className,
	...props
}: React.ComponentProps<typeof TogglePrimitive>) {
	return (
		<TogglePrimitive
			data-slot="toggle-group-item"
			className={cn(
				"inline-flex items-center justify-center gap-1.5 rounded-sm px-2.5 h-7 text-xs font-medium text-muted-foreground transition-colors duration-75 outline-none",
				"hover:bg-accent hover:text-accent-foreground",
				"data-[pressed]:bg-primary data-[pressed]:text-primary-foreground",
				"focus-visible:ring-2 focus-visible:ring-ring",
				"disabled:pointer-events-none disabled:opacity-50",
				className,
			)}
			{...props}
		/>
	);
}

export { ToggleGroup, ToggleGroupItem };
