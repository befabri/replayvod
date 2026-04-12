import { Switch as SwitchPrimitive } from "@base-ui/react/switch"
import type * as React from "react"

import { cn } from "@/lib/utils"

function Switch({ className, ...props }: React.ComponentProps<typeof SwitchPrimitive.Root>) {
	return (
		<SwitchPrimitive.Root
			data-slot="switch"
			className={cn(
				"peer inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border border-transparent shadow-xs transition-all outline-none",
				"focus-visible:ring-[3px] focus-visible:ring-ring/50",
				"disabled:cursor-not-allowed disabled:opacity-50",
				"data-[checked]:bg-primary data-[unchecked]:bg-input",
				className,
			)}
			{...props}
		>
			<SwitchPrimitive.Thumb
				data-slot="switch-thumb"
				className={cn(
					"pointer-events-none block size-4 rounded-full bg-background shadow-sm transition-transform",
					"data-[checked]:translate-x-4 data-[unchecked]:translate-x-0.5",
				)}
			/>
		</SwitchPrimitive.Root>
	)
}

export { Switch }
