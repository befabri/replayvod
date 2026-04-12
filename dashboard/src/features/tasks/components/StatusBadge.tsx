import { useTranslation } from "react-i18next"

export function StatusBadge({
	status,
	enabled,
	error,
}: {
	status: string
	enabled: boolean
	error?: string
}) {
	const { t } = useTranslation()
	if (!enabled) {
		return (
			<span className="inline-flex items-center rounded-md px-2 py-0.5 text-xs bg-muted text-muted-foreground">
				{t("tasks.status_paused")}
			</span>
		)
	}
	const variant =
		{
			success: "bg-primary/20 text-primary-foreground",
			failed: "bg-destructive/20 text-destructive",
			running: "bg-primary/20 text-primary-foreground animate-pulse",
			pending: "bg-muted text-muted-foreground",
			skipped: "bg-muted text-muted-foreground",
		}[status] ?? "bg-muted text-muted-foreground"
	return (
		<>
			<span
				className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs ${variant}`}
			>
				{t(`tasks.status_${status}`, { defaultValue: status })}
			</span>
			{error && (
				<div
					className="text-xs text-destructive mt-1 max-w-sm truncate"
					title={error}
				>
					{error}
				</div>
			)}
		</>
	)
}
