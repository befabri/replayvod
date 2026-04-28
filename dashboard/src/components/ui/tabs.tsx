import { Tabs as TabsPrimitive } from "@base-ui/react/tabs";
import type * as React from "react";

import { cn } from "@/lib/utils";

function Tabs(props: React.ComponentProps<typeof TabsPrimitive.Root>) {
	return <TabsPrimitive.Root data-slot="tabs" {...props} />;
}

function TabsList({
	className,
	...props
}: React.ComponentProps<typeof TabsPrimitive.List>) {
	return (
		<TabsPrimitive.List
			data-slot="tabs-list"
			className={cn(
				"inline-flex h-9 items-center justify-center rounded-md bg-muted p-1 text-muted-foreground",
				className,
			)}
			{...props}
		/>
	);
}

function TabsTrigger({
	className,
	...props
}: React.ComponentProps<typeof TabsPrimitive.Tab>) {
	return (
		<TabsPrimitive.Tab
			data-slot="tabs-trigger"
			className={cn(
				"inline-flex items-center justify-center whitespace-nowrap rounded-sm px-3 py-1 text-sm font-medium transition-all outline-none",
				"focus-visible:ring-[3px] focus-visible:ring-ring/50",
				"disabled:pointer-events-none disabled:opacity-50",
				// Base UI's Tab primitive emits `data-active` on the
				// selected tab — `data-selected` (the shadcn/Radix
				// convention) never matches under @base-ui/react and
				// the active style was silently never rendering.
				"data-[active]:bg-background data-[active]:text-foreground data-[active]:shadow-sm",
				className,
			)}
			{...props}
		/>
	);
}

function TabsContent({
	className,
	...props
}: React.ComponentProps<typeof TabsPrimitive.Panel>) {
	return (
		<TabsPrimitive.Panel
			data-slot="tabs-content"
			className={cn(
				"mt-2 outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50",
				className,
			)}
			{...props}
		/>
	);
}

export { Tabs, TabsList, TabsTrigger, TabsContent };
