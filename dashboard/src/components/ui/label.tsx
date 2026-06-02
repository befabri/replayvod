import type * as React from "react";

import { cn } from "@/lib/utils";

function Label({ className, ...props }: React.ComponentProps<"label">) {
	return (
		// biome-ignore lint/a11y/noLabelWithoutControl: reusable label primitive — consumers associate it with a control via htmlFor (or by nesting the input), which this generic wrapper can't know statically.
		<label
			data-slot="label"
			className={cn(
				"flex items-center gap-2 text-sm font-medium leading-none select-none",
				"group-data-[disabled=true]:pointer-events-none group-data-[disabled=true]:opacity-50",
				"peer-disabled:cursor-not-allowed peer-disabled:opacity-50",
				className,
			)}
			{...props}
		/>
	);
}

export { Label };
