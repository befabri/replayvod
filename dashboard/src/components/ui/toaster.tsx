import { useStore } from "@tanstack/react-store";
import { Toaster as Sonner, type ToasterProps } from "sonner";
import { themeStore } from "@/stores/theme";

const TOAST_OPTIONS: ToasterProps["toastOptions"] = {
	classNames: {
		toast:
			"rounded-md bg-popover text-popover-foreground border border-border shadow-md",
		title: "text-sm font-medium",
		description: "text-xs text-muted-foreground",
		actionButton:
			"bg-primary text-primary-foreground rounded-md px-2 py-1 text-xs",
		cancelButton: "bg-muted text-muted-foreground rounded-md px-2 py-1 text-xs",
		// Status variants get a tinted bg so they read as status at a
		// glance; text color matches so the pair stays legible.
		success: "bg-badge-green-bg text-badge-green-fg",
		error: "bg-destructive/15 text-destructive",
		warning: "bg-badge-yellow-bg text-badge-yellow-fg",
		info: "bg-badge-blue-bg text-badge-blue-fg",
	},
};

export function Toaster(props: ToasterProps) {
	const theme = useStore(themeStore, (s) => s.theme);
	return (
		<Sonner
			theme={theme}
			position="top-right"
			toastOptions={TOAST_OPTIONS}
			{...props}
		/>
	);
}

export { toast } from "sonner";
