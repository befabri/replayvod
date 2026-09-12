import { TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { useDeleteVideo } from "@/features/videos";
import { cn } from "@/lib/utils";

// RemoveVideoButton is the operator-facing delete control for a recording. It
// owns the confirm step and the video.delete mutation so every surface (table
// row, grid card, watch page) drops in the same action. Clicks are stopped from
// bubbling so it can sit safely inside link-wrapped cards.
//
// It does not gate on permission itself: each surface checks useCanManageVideos
// once and omits the button for viewers, so a read-only list never mounts a
// mutation + i18n subscription per row just to render null.
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
	// permanent removes a missing-media tombstone for good: the confirm names
	// what is left to delete (previews), since the media is already gone.
	permanent?: boolean;
	// onRemoved fires after a successful delete — the watch page uses it to
	// navigate away from the now-removed recording.
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
