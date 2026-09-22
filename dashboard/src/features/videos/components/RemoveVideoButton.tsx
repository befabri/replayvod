import { TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { useDeleteVideo } from "@/features/videos";
import { cn } from "@/lib/utils";

export function RemoveVideoButton({
	videoId,
	withLabel = false,
	className,
	onRemoved,
	permanent = false,
}: {
	videoId: number;
	withLabel?: boolean;
	className?: string;
	permanent?: boolean;
	onRemoved?: () => void;
}) {
	const { t } = useTranslation();
	const [open, setOpen] = useState(false);
	const remove = useDeleteVideo();

	const confirm = () => {
		remove.mutate(
			{ id: videoId },
			{
				onSuccess: () => {
					setOpen(false);
					onRemoved?.();
				},
				onError: (err) => {
					toast.error(err.message || t("videos.remove_failed"));
				},
			},
		);
	};

	return (
		<>
			<button
				type="button"
				onClick={(e) => {
					e.stopPropagation();
					e.preventDefault();
					setOpen(true);
				}}
				aria-label={t("videos.remove")}
				title={t("videos.remove")}
				className={cn(
					withLabel
						? "inline-flex items-center gap-1.5 text-xs text-destructive hover:underline"
						: "inline-flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-destructive/15 hover:text-destructive",
					className,
				)}
			>
				<TrashIcon className="size-4" />
				{withLabel ? t("videos.remove") : null}
			</button>
			{open ? (
				<ConfirmDialog
					open={open}
					onOpenChange={setOpen}
					title={t("videos.remove_confirm_title")}
					description={t(
						permanent
							? "videos.remove_permanent_confirm_body"
							: "videos.remove_confirm_body",
					)}
					confirmLabel={t("videos.remove_confirm")}
					cancelLabel={t("common.cancel")}
					onConfirm={confirm}
					confirming={remove.isPending}
					destructive
				/>
			) : null}
		</>
	);
}
