export function SeverityBadge({ severity }: { severity: string }) {
	const cls =
		{
			debug: "bg-muted text-muted-foreground",
			info: "bg-muted text-foreground",
			warn: "bg-yellow-500/20 text-yellow-200",
			error: "bg-destructive/20 text-destructive",
		}[severity] ?? "bg-muted text-muted-foreground"
	return (
		<span
			className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs ${cls}`}
		>
			{severity}
		</span>
	)
}
