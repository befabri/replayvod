import { useTranslation } from "react-i18next";

export function VideoStatusBadge({
	status,
	completionKind,
}: {
	status: string;
	completionKind?: string;
}) {
	const { t } = useTranslation();
	const isCancelled = status === "FAILED" && completionKind === "cancelled";
	const isPartial = status === "DONE" && completionKind === "partial";
	const cls = isCancelled
		? "bg-muted text-muted-foreground"
		: ({
				DONE: "bg-badge-green-bg text-badge-green-fg",
				FAILED: "bg-badge-red-bg text-badge-red-fg",
				RUNNING: "bg-badge-blue-bg text-badge-blue-fg animate-pulse",
				PENDING: "bg-muted text-muted-foreground",
			}[status] ?? "bg-muted text-muted-foreground");
	const label = isCancelled
		? t("videos.status.CANCELLED", "CANCELLED")
		: t(`videos.status.${status}` as const, status);

	return (
		<span className="inline-flex flex-wrap items-center gap-1.5">
			<span
				className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs ${cls}`}
			>
				{label}
			</span>
			{isPartial ? (
				<span className="inline-flex items-center rounded-md bg-badge-yellow-bg px-2 py-0.5 text-xs text-badge-yellow-fg">
					{t("videos.completion.partial", "PARTIAL")}
				</span>
			) : null}
		</span>
	);
}
