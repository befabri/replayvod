import { TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { useDeleteVideo } from "@/features/videos";

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
			<Button
				variant={withLabel ? "destructive" : "ghost-destructive"}
				size={withLabel ? "sm" : "icon-sm"}
				onClick={(e) => {
					e.stopPropagation();
					e.preventDefault();
					setOpen(true);
				}}
				aria-label={t("videos.remove")}
				title={t("videos.remove")}
				className={className}
			>
				<TrashIcon data-icon={withLabel ? "inline-start" : undefined} />
				{withLabel ? t("videos.remove") : null}
			</Button>
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
