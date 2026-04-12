export function VideoStatusBadge({ status }: { status: string }) {
	const cls =
		{
			DONE: "bg-emerald-500/20 text-emerald-100",
			FAILED: "bg-destructive/20 text-destructive",
			RUNNING: "bg-primary/20 text-primary-foreground animate-pulse",
			PENDING: "bg-muted text-muted-foreground",
		}[status] ?? "bg-muted text-muted-foreground"
	return (
		<span
			className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs ${cls}`}
		>
			{status}
		</span>
	)
}
