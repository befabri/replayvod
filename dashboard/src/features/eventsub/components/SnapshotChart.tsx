export function SnapshotChart({
	data,
}: {
	data: { id: number; fetched_at: string; total_cost: number; total: number }[]
}) {
	// Newest first from the server; reverse for left-to-right chronological.
	const points = [...data].reverse()
	const maxCost = Math.max(1, ...points.map((p) => p.total_cost))

	return (
		<div className="rounded-lg border border-border bg-card p-4">
			<div className="flex items-end gap-1 h-24">
				{points.map((p) => {
					const h = (p.total_cost / maxCost) * 100
					return (
						<div
							key={p.id}
							title={`${new Date(p.fetched_at).toLocaleString()} — cost ${p.total_cost}`}
							className="flex-1 min-w-0 bg-primary/70 rounded-sm"
							style={{ height: `${h}%` }}
						/>
					)
				})}
			</div>
			<div className="mt-2 text-xs text-muted-foreground">
				{points.length > 0 && (
					<>
						{new Date(points[0].fetched_at).toLocaleDateString()} →{" "}
						{new Date(points[points.length - 1].fetched_at).toLocaleDateString()}
					</>
				)}
			</div>
		</div>
	)
}
