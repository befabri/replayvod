import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";

// ConfirmDialog is a controlled yes/no confirmation for an action that warrants
// a deliberate second step (e.g. an irreversible delete). The caller owns the
// open state and runs the action in onConfirm; `confirming` disables both
// buttons while the action is in flight.
export function ConfirmDialog({
	open,
	onOpenChange,
	title,
	description,
	confirmLabel,
	cancelLabel,
	onConfirm,
	confirming = false,
	destructive = false,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	title: string;
	description?: string;
	confirmLabel: string;
	cancelLabel: string;
	onConfirm: () => void;
	confirming?: boolean;
	destructive?: boolean;
}) {
	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-w-md">
				<DialogHeader>
					<DialogTitle>{title}</DialogTitle>
					{description ? (
						<DialogDescription>{description}</DialogDescription>
					) : null}
				</DialogHeader>
				<DialogFooter>
					<Button
						variant="outline"
						onClick={() => onOpenChange(false)}
						disabled={confirming}
					>
						{cancelLabel}
					</Button>
					<Button
						variant={destructive ? "destructive" : "default"}
						onClick={onConfirm}
						disabled={confirming}
					>
						{confirmLabel}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
