import { useTranslation } from "react-i18next";
import type { CompletionKind, VideoStatus } from "@/api/generated/trpc";
import { Badge } from "@/components/ui/badge";
import { videoStatusLabel } from "@/features/videos/labels";

// Record<VideoStatus, ...> so a new generated status is a compile error here
// rather than silently falling through to an undefined variant. Maps each
// status onto a shared Badge color variant.
const STATUS_VARIANT: Record<VideoStatus, "green" | "red" | "blue" | "muted"> =
	{
		DONE: "green",
		FAILED: "red",
		RUNNING: "blue",
		PENDING: "muted",
	};

export function VideoStatusBadge({
	status,
	completionKind,
}: {
	status: VideoStatus;
	completionKind?: CompletionKind;
}) {
	const { t } = useTranslation();
	const isCancelled = status === "FAILED" && completionKind === "cancelled";
	const isPartial = status === "DONE" && completionKind === "partial";
	const label = isCancelled
		? videoStatusLabel(t, "CANCELLED")
		: videoStatusLabel(t, status);

	return (
		<span className="inline-flex flex-wrap items-center gap-1.5">
			<Badge
				variant={isCancelled ? "muted" : STATUS_VARIANT[status]}
				className={status === "RUNNING" ? "animate-pulse" : undefined}
			>
				{label}
			</Badge>
			{isPartial ? (
				<Badge variant="yellow">
					{t("videos.completion.partial", "PARTIAL")}
				</Badge>
			) : null}
		</span>
	);
}
