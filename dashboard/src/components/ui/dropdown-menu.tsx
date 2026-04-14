import { Menu as MenuPrimitive } from "@base-ui/react/menu";
import { CaretRight, Check } from "@phosphor-icons/react";
import type * as React from "react";

import { cn } from "@/lib/utils";

function DropdownMenu(props: React.ComponentProps<typeof MenuPrimitive.Root>) {
	return <MenuPrimitive.Root {...props} />;
}

function DropdownMenuTrigger(
	props: React.ComponentProps<typeof MenuPrimitive.Trigger>,
) {
	return <MenuPrimitive.Trigger data-slot="dropdown-menu-trigger" {...props} />;
}

function DropdownMenuContent({
	className,
	sideOffset = 6,
	align = "end",
	children,
	...props
}: React.ComponentProps<typeof MenuPrimitive.Popup> & {
	sideOffset?: number;
	align?: "start" | "center" | "end";
}) {
	return (
		<MenuPrimitive.Portal>
			<MenuPrimitive.Positioner
				sideOffset={sideOffset}
				align={align}
				className="z-50"
			>
				<MenuPrimitive.Popup
					data-slot="dropdown-menu-content"
					className={cn(
						"min-w-48 rounded-md border border-border bg-popover text-popover-foreground shadow-md outline-none",
						"p-1",
						"data-[open]:animate-in data-[closed]:animate-out data-[closed]:fade-out-0 data-[open]:fade-in-0 data-[closed]:zoom-out-95 data-[open]:zoom-in-95",
						className,
					)}
					{...props}
				>
					{children}
				</MenuPrimitive.Popup>
			</MenuPrimitive.Positioner>
		</MenuPrimitive.Portal>
	);
}

function DropdownMenuItem({
	className,
	...props
}: React.ComponentProps<typeof MenuPrimitive.Item>) {
	return (
		<MenuPrimitive.Item
			data-slot="dropdown-menu-item"
			className={cn(
				"flex cursor-default select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none",
				"data-[highlighted]:bg-primary data-[highlighted]:text-primary-foreground",
				"data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
				className,
			)}
			{...props}
		/>
	);
}

function DropdownMenuCheckboxItem({
	className,
	checked,
	children,
	...props
}: React.ComponentProps<typeof MenuPrimitive.CheckboxItem>) {
	return (
		<MenuPrimitive.CheckboxItem
			data-slot="dropdown-menu-checkbox-item"
			checked={checked}
			className={cn(
				"relative flex cursor-default select-none items-center gap-2 rounded-sm py-1.5 pl-8 pr-2 text-sm outline-none",
				"data-[highlighted]:bg-primary data-[highlighted]:text-primary-foreground",
				"data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
				className,
			)}
			{...props}
		>
			<span className="absolute left-2 flex size-4 items-center justify-center">
				<MenuPrimitive.CheckboxItemIndicator>
					<Check className="size-4" />
				</MenuPrimitive.CheckboxItemIndicator>
			</span>
			{children}
		</MenuPrimitive.CheckboxItem>
	);
}

function DropdownMenuSubmenu(
	props: React.ComponentProps<typeof MenuPrimitive.SubmenuRoot>,
) {
	return <MenuPrimitive.SubmenuRoot {...props} />;
}

function DropdownMenuSubmenuTrigger({
	className,
	children,
	...props
}: React.ComponentProps<typeof MenuPrimitive.SubmenuTrigger>) {
	return (
		<MenuPrimitive.SubmenuTrigger
			data-slot="dropdown-menu-submenu-trigger"
			className={cn(
				"flex cursor-default select-none items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-sm outline-none",
				"data-[highlighted]:bg-primary data-[highlighted]:text-primary-foreground",
				"data-[popup-open]:bg-primary data-[popup-open]:text-primary-foreground",
				className,
			)}
			{...props}
		>
			<span className="flex items-center gap-2">{children}</span>
			<CaretRight className="size-4 opacity-70" />
		</MenuPrimitive.SubmenuTrigger>
	);
}

function DropdownMenuSeparator({
	className,
	...props
}: React.ComponentProps<typeof MenuPrimitive.Separator>) {
	return (
		<MenuPrimitive.Separator
			data-slot="dropdown-menu-separator"
			className={cn("my-1 h-px bg-border", className)}
			{...props}
		/>
	);
}

function DropdownMenuLabel({
	className,
	...props
}: React.ComponentProps<"div">) {
	return (
		<div
			data-slot="dropdown-menu-label"
			className={cn(
				"px-2 py-1.5 text-xs font-medium text-muted-foreground",
				className,
			)}
			{...props}
		/>
	);
}

export {
	DropdownMenu,
	DropdownMenuTrigger,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuCheckboxItem,
	DropdownMenuSubmenu,
	DropdownMenuSubmenuTrigger,
	DropdownMenuSeparator,
	DropdownMenuLabel,
};
