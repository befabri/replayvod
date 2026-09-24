import { Combobox as ComboboxPrimitive } from "@base-ui/react/combobox";
import { CaretDownIcon, CheckIcon, XIcon } from "@phosphor-icons/react";
import type * as React from "react";
import { createContext, useContext } from "react";

import { type PopupLabel, usePopupLabel } from "@/lib/element-label";
import { cn } from "@/lib/utils";

const ComboboxLabelContext = createContext<PopupLabel<HTMLInputElement> | null>(
	null,
);

const ComboboxDisabledContext = createContext(false);

function Combobox<Value, Multiple extends boolean | undefined = false>(
	props: ComboboxPrimitive.Root.Props<Value, Multiple>,
) {
	const labelling = usePopupLabel<HTMLInputElement>();
	return (
		<ComboboxLabelContext value={labelling}>
			<ComboboxDisabledContext value={props.disabled ?? false}>
				<ComboboxPrimitive.Root {...props} />
			</ComboboxDisabledContext>
		</ComboboxLabelContext>
	);
}

function ComboboxInput({
	className,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.Input>) {
	const anchorRef = useContext(ComboboxLabelContext)?.anchorRef;
	return (
		<ComboboxPrimitive.Input
			ref={anchorRef}
			data-slot="combobox-input"
			className={cn(
				"flex h-9 w-full items-center rounded-md border border-border bg-background px-3 py-1 text-sm shadow-xs outline-none",
				"focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50",
				"disabled:cursor-not-allowed not-in-data-dimmed:disabled:opacity-50",
				"aria-invalid:border-destructive aria-invalid:ring-[3px] aria-invalid:ring-destructive/20",
				className,
			)}
			{...props}
		/>
	);
}

function ComboboxChipsInput({
	className,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.Input>) {
	return (
		<ComboboxInput
			data-slot="combobox-chips-input"
			className={cn(
				"h-auto min-w-[8rem] flex-1 border-0 bg-transparent px-0 py-0 shadow-none",
				"focus-visible:border-0 focus-visible:ring-0",
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
			{children ?? <CaretDownIcon className="size-4 opacity-70" />}
		</ComboboxPrimitive.Trigger>
	);
}

function ComboboxContent({
	className,
	children,
	sideOffset = 4,
	anchor,
	align,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.Popup> & {
	sideOffset?: number;
	anchor?: React.ComponentProps<typeof ComboboxPrimitive.Positioner>["anchor"];
	align?: React.ComponentProps<typeof ComboboxPrimitive.Positioner>["align"];
}) {
	return (
		<ComboboxPrimitive.Portal>
			<ComboboxPrimitive.Positioner
				sideOffset={sideOffset}
				anchor={anchor}
				align={align}
				className="z-50"
			>
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
	children: React.ReactNode | ((item: T, index: number) => React.ReactNode);
};

function ComboboxList<T = unknown>(props: ComboboxListProps<T>) {
	const labelling = useContext(ComboboxLabelContext);
	return (
		<ComboboxPrimitive.List
			ref={labelling?.popupRef}
			aria-label={labelling?.label}
			{...props}
		/>
	);
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
				<CheckIcon className="size-4" />
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

function ComboboxGroup({
	className,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.Group>) {
	return (
		<ComboboxPrimitive.Group
			data-slot="combobox-group"
			className={cn("py-1", className)}
			{...props}
		/>
	);
}

function ComboboxGroupLabel({
	className,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.GroupLabel>) {
	return (
		<ComboboxPrimitive.GroupLabel
			data-slot="combobox-group-label"
			className={cn(
				"flex items-center justify-between px-2 pb-1 pt-1.5 text-xs font-medium uppercase text-muted-foreground",
				className,
			)}
			{...props}
		/>
	);
}

type ComboboxCollectionProps<T> = Omit<
	React.ComponentProps<typeof ComboboxPrimitive.Collection>,
	"children"
> & {
	children: (item: T, index: number) => React.ReactNode;
};

function ComboboxCollection<T = unknown>(props: ComboboxCollectionProps<T>) {
	return <ComboboxPrimitive.Collection {...props} />;
}

function ComboboxChips({
	className,
	...props
}: React.ComponentProps<typeof ComboboxPrimitive.Chips>) {
	const disabled = useContext(ComboboxDisabledContext);
	return (
		<ComboboxPrimitive.Chips
			data-slot="combobox-chips"
			data-dimmed={disabled || undefined}
			className={cn(
				"flex flex-wrap items-center gap-1.5 rounded-md border border-border bg-background px-2 py-1.5 min-h-9 text-sm shadow-xs outline-none",
				"focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/50",
				"data-dimmed:pointer-events-none data-dimmed:opacity-50",
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
			<XIcon className="size-3" />
		</ComboboxPrimitive.ChipRemove>
	);
}

export {
	Combobox,
	ComboboxChip,
	ComboboxChipRemove,
	ComboboxChips,
	ComboboxChipsInput,
	ComboboxCollection,
	ComboboxContent,
	ComboboxEmpty,
	ComboboxGroup,
	ComboboxGroupLabel,
	ComboboxInput,
	ComboboxItem,
	ComboboxList,
	ComboboxStatus,
	ComboboxTrigger,
};
