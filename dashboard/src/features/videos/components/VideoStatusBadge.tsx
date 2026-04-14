export function VideoStatusBadge({ status }: { status: string }) {
	const cls =
		{
			DONE: "bg-badge-green-bg text-badge-green-fg",
			FAILED: "bg-badge-red-bg text-badge-red-fg",
			RUNNING: "bg-badge-blue-bg text-badge-blue-fg animate-pulse",
			PENDING: "bg-muted text-muted-foreground",
		}[status] ?? "bg-muted text-muted-foreground";
	return (
		<span
			className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs ${cls}`}
		>
			{status}
		</span>
	);
}
