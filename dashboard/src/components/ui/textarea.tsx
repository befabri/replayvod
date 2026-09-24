import type * as React from "react";

import { cn } from "@/lib/utils";

function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
	return (
		<textarea
			data-slot="textarea"
			className={cn(
				"flex min-h-16 w-full min-w-0 rounded-md border border-border bg-background px-3 py-2 text-sm shadow-xs transition-colors outline-none",
				"placeholder:text-muted-foreground",
				"focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50",
				"disabled:pointer-events-none disabled:cursor-not-allowed not-in-data-dimmed:disabled:opacity-50",
				"aria-invalid:border-destructive aria-invalid:ring-[3px] aria-invalid:ring-destructive/20",
				className,
			)}
			{...props}
		/>
	);
}

export { Textarea };
