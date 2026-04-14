export function SeverityBadge({ severity }: { severity: string }) {
	const cls =
		{
			debug: "bg-muted text-muted-foreground",
			info: "bg-muted text-foreground",
			warn: "bg-badge-yellow-bg text-badge-yellow-fg",
			error: "bg-badge-red-bg text-badge-red-fg",
		}[severity] ?? "bg-muted text-muted-foreground";
	return (
		<span
			className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs ${cls}`}
		>
			{severity}
		</span>
	);
}
