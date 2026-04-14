import { Combobox as ComboboxPrimitive } from "@base-ui/react/combobox";
import { CaretDown, Check, X } from "@phosphor-icons/react";
import type * as React from "react";

import { cn } from "@/lib/utils";

// Thin shadcn-style wrappers over Base UI Combobox. Pass `items` directly
// (server-side filtered results) and Base UI's default filter = items.
// Callers that want client-side filtering can pass their own via `filter` prop.

function Combobox<Value, Multiple extends boolean | undefined = false>(
	props: ComboboxPrimitive.Root.Props<Value, Multiple>,
) {
	return <ComboboxPrimitive.Root {...props} />;
}

function ComboboxInput({
	className,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.Input>) {
	return (
		<ComboboxPrimitive.Input
			data-slot="combobox-input"
			className={cn(
				"flex h-9 w-full items-center rounded-md border border-border bg-background px-3 py-1 text-sm shadow-xs outline-none",
				"focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50",
				"disabled:cursor-not-allowed disabled:opacity-50",
				"aria-invalid:border-destructive aria-invalid:ring-[3px] aria-invalid:ring-destructive/20",
				className,
			)}
			{...props}
		/>
	);
}

function ComboboxTrigger({
	className,
	children,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.Trigger>) {
	return (
		<ComboboxPrimitive.Trigger
			data-slot="combobox-trigger"
			className={cn(
				"inline-flex items-center justify-center rounded-sm p-1 text-muted-foreground hover:text-foreground outline-none",
				className,
			)}
			{...props}
		>
			{children ?? <CaretDown className="size-4 opacity-70" />}
		</ComboboxPrimitive.Trigger>
	);
}

function ComboboxContent({
	className,
	children,
	sideOffset = 4,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.Popup> & {
	sideOffset?: number;
}) {
	return (
		<ComboboxPrimitive.Portal>
			<ComboboxPrimitive.Positioner sideOffset={sideOffset} className="z-50">
				<ComboboxPrimitive.Popup
					data-slot="combobox-content"
					className={cn(
						"max-h-[var(--available-height)] min-w-[var(--anchor-width)] overflow-y-auto rounded-md border border-border bg-popover text-popover-foreground shadow-md outline-none p-1",
						"data-[open]:animate-in data-[closed]:animate-out data-[closed]:fade-out-0 data-[open]:fade-in-0 data-[closed]:zoom-out-95 data-[open]:zoom-in-95",
						className,
					)}
					{...props}
				>
					{children}
				</ComboboxPrimitive.Popup>
			</ComboboxPrimitive.Positioner>
		</ComboboxPrimitive.Portal>
	);
}

type ComboboxListProps<T> = Omit<
	React.ComponentProps<typeof ComboboxPrimitive.List>,
	"children"
> & {
	children: (item: T, index: number) => React.ReactNode;
};

function ComboboxList<T = unknown>(props: ComboboxListProps<T>) {
	return <ComboboxPrimitive.List {...props} />;
}

function ComboboxItem({
	className,
	children,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.Item>) {
	return (
		<ComboboxPrimitive.Item
			data-slot="combobox-item"
			className={cn(
				"relative flex cursor-default select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none",
				"data-[highlighted]:bg-primary data-[highlighted]:text-primary-foreground",
				"data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
				className,
			)}
			{...props}
		>
			{children}
			<ComboboxPrimitive.ItemIndicator className="ml-auto">
				<Check className="size-4" />
			</ComboboxPrimitive.ItemIndicator>
		</ComboboxPrimitive.Item>
	);
}

function ComboboxEmpty({
	className,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.Empty>) {
	return (
		<ComboboxPrimitive.Empty
			data-slot="combobox-empty"
			className={cn(
				"px-2 py-3 text-center text-sm text-muted-foreground",
				className,
			)}
			{...props}
		/>
	);
}

function ComboboxStatus({
	className,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.Status>) {
	return (
		<ComboboxPrimitive.Status
			data-slot="combobox-status"
			className={cn(
				"px-2 py-3 text-center text-sm text-muted-foreground",
				className,
			)}
			{...props}
		/>
	);
}

function ComboboxChips({
	className,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.Chips>) {
	return (
		<ComboboxPrimitive.Chips
			data-slot="combobox-chips"
			className={cn(
				"flex flex-wrap items-center gap-1.5 rounded-md border border-border bg-background px-2 py-1.5 min-h-9 text-sm shadow-xs outline-none",
				"focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/50",
				className,
			)}
			{...props}
		/>
	);
}

function ComboboxChip({
	className,
	children,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.Chip>) {
	return (
		<ComboboxPrimitive.Chip
			data-slot="combobox-chip"
			className={cn(
				"inline-flex items-center gap-1 rounded-md bg-primary/20 text-foreground px-2 py-0.5 text-xs",
				className,
			)}
			{...props}
		>
			{children}
		</ComboboxPrimitive.Chip>
	);
}

function ComboboxChipRemove({
	className,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.ChipRemove>) {
	return (
		<ComboboxPrimitive.ChipRemove
			data-slot="combobox-chip-remove"
			className={cn(
				"rounded-sm opacity-60 hover:opacity-100 transition-opacity",
				className,
			)}
			aria-label="Remove"
			{...props}
		>
			<X className="size-3" />
		</ComboboxPrimitive.ChipRemove>
	);
}

export {
	Combobox,
	ComboboxInput,
	ComboboxTrigger,
	ComboboxContent,
	ComboboxList,
	ComboboxItem,
	ComboboxEmpty,
	ComboboxStatus,
	ComboboxChips,
	ComboboxChip,
	ComboboxChipRemove,
};
