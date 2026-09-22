import { ArrowCounterClockwiseIcon } from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { useRestoreVideo } from "@/features/videos/queries";
import { cn } from "@/lib/utils";

export function RestoreVideoButton({
	videoId,
	withLabel = false,
	className,
	onRestored,
}: {
	videoId: number;
	withLabel?: boolean;
	className?: string;
	onRestored?: () => void;
}) {
	const { t } = useTranslation();
	const restore = useRestoreVideo();

	const handleRestore = () => {
		restore.mutate(
			{ id: videoId },
			{
				onSuccess: () => {
					toast.success(t("videos.restored_toast"));
					onRestored?.();
				},
				onError: (err) => {
					toast.error(
						err instanceof Error ? err.message : t("videos.restore_failed"),
					);
				},
			},
		);
	};

	return (
		<button
			type="button"
			onClick={(e) => {
				e.stopPropagation();
				e.preventDefault();
				handleRestore();
			}}
			disabled={restore.isPending}
			aria-label={t("videos.restore")}
			title={t("videos.restore")}
			className={cn(
				withLabel
					? "inline-flex items-center gap-1.5 text-xs text-link hover:underline disabled:opacity-50"
					: "inline-flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:opacity-50",
				className,
			)}
		>
			<ArrowCounterClockwiseIcon className="size-4" />
			{withLabel ? t("videos.restore") : null}
		</button>
	);
}
