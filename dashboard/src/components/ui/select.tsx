import { Select as SelectPrimitive } from "@base-ui/react/select";
import { CaretDownIcon, CheckIcon } from "@phosphor-icons/react";
import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";
import { createContext, useContext } from "react";

import { type PopupLabel, usePopupLabel } from "@/lib/element-label";
import { cn } from "@/lib/utils";

const selectTriggerVariants = cva(
	"flex h-9 items-center justify-between gap-2 rounded-md border border-border px-3 py-1 text-sm outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed not-in-data-dimmed:disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-[3px] aria-invalid:ring-destructive/20",
	{
		variants: {
			variant: {
				default: "w-full bg-background shadow-xs",
				chip: "w-auto border-transparent bg-card hover:bg-accent hover:text-accent-foreground",
			},
		},
		defaultVariants: { variant: "default" },
	},
);

const SelectLabelContext = createContext<PopupLabel<HTMLButtonElement> | null>(
	null,
);

function Select(props: React.ComponentProps<typeof SelectPrimitive.Root>) {
	const labelling = usePopupLabel<HTMLButtonElement>();
	return (
		<SelectLabelContext value={labelling}>
			<SelectPrimitive.Root data-slot="select" {...props} />
		</SelectLabelContext>
	);
}

function SelectValue(
	props: React.ComponentProps<typeof SelectPrimitive.Value>,
) {
	return <SelectPrimitive.Value data-slot="select-value" {...props} />;
}

function SelectTrigger({
	className,
	variant,
	children,
	...props
}: React.ComponentProps<typeof SelectPrimitive.Trigger> &
	VariantProps<typeof selectTriggerVariants>) {
	const anchorRef = useContext(SelectLabelContext)?.anchorRef;
	return (
		<SelectPrimitive.Trigger
			ref={anchorRef}
			data-slot="select-trigger"
			className={cn(selectTriggerVariants({ variant }), className)}
			{...props}
		>
			{children}
			<SelectPrimitive.Icon>
				<CaretDownIcon className="size-4 opacity-50" />
			</SelectPrimitive.Icon>
		</SelectPrimitive.Trigger>
	);
}

function SelectContent({
	className,
	children,
	...props
}: React.ComponentProps<typeof SelectPrimitive.Popup>) {
	const labelling = useContext(SelectLabelContext);
	return (
		<SelectPrimitive.Portal>
			<SelectPrimitive.Positioner
				side="bottom"
				alignItemWithTrigger={false}
				className="z-50"
			>
				<SelectPrimitive.Popup
					ref={labelling?.popupRef}
					aria-label={labelling?.label}
					data-slot="select-content"
					className={cn(
						"max-h-[var(--available-height)] min-w-[var(--anchor-width)] overflow-y-auto rounded-md border border-border bg-popover text-popover-foreground shadow-md",
						"data-[open]:animate-in data-[closed]:animate-out data-[closed]:fade-out-0 data-[open]:fade-in-0 data-[closed]:zoom-out-95 data-[open]:zoom-in-95",
						className,
					)}
					{...props}
				>
					{children}
				</SelectPrimitive.Popup>
			</SelectPrimitive.Positioner>
		</SelectPrimitive.Portal>
	);
}

function SelectItem({
	className,
	children,
	...props
}: React.ComponentProps<typeof SelectPrimitive.Item>) {
	return (
		<SelectPrimitive.Item
			data-slot="select-item"
			className={cn(
				"flex cursor-default select-none items-center gap-2 py-1.5 pr-2 pl-8 text-sm outline-none",
				"data-[highlighted]:bg-muted data-[highlighted]:text-foreground",
				"data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
				className,
			)}
			{...props}
		>
			<span className="absolute left-2 flex size-3.5 items-center justify-center">
				<SelectPrimitive.ItemIndicator>
					<CheckIcon className="size-4" />
				</SelectPrimitive.ItemIndicator>
			</span>
			<SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
		</SelectPrimitive.Item>
	);
}

export { Select, SelectContent, SelectItem, SelectTrigger, SelectValue };
