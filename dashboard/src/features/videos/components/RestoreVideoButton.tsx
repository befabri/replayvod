import { ArrowCounterClockwiseIcon } from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { useRestoreVideo } from "@/features/videos/queries";

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
		<Button
			variant={withLabel ? "outline" : "ghost-muted"}
			size={withLabel ? "sm" : "icon-sm"}
			onClick={(e) => {
				e.stopPropagation();
				e.preventDefault();
				handleRestore();
			}}
			disabled={restore.isPending}
			aria-label={t("videos.restore")}
			title={t("videos.restore")}
			className={className}
		>
			<ArrowCounterClockwiseIcon
				data-icon={withLabel ? "inline-start" : undefined}
			/>
			{withLabel ? t("videos.restore") : null}
		</Button>
	);
}
