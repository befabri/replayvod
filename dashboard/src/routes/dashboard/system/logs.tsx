import { createFileRoute } from "@tanstack/react-router"
import { useState } from "react"
import { useFetchLogs } from "@/features/system"

const PAGE_SIZE = 50

export const Route = createFileRoute("/dashboard/system/logs")({
	component: SystemLogsPage,
})

function SystemLogsPage() {
	const [page, setPage] = useState(0)
	const { data, isLoading, error } = useFetchLogs(PAGE_SIZE, page * PAGE_SIZE)

	const total = data?.total ?? 0
	const logs = data?.data ?? []
	const pageCount = Math.ceil(total / PAGE_SIZE)

	return (
		<div className="p-8">
			<h1 className="text-3xl font-heading font-bold mb-6">API Fetch Logs</h1>

			{isLoading && <div className="text-muted-foreground">Loading…</div>}

			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					Failed to load logs: {error.message}
				</div>
			)}

			{logs.length === 0 && !isLoading && !error && (
				<div className="text-muted-foreground">No fetch logs yet.</div>
			)}

			{logs.length > 0 && (
				<>
					<div className="rounded-lg border border-border overflow-hidden">
						<table className="w-full text-sm">
							<thead className="bg-muted/50">
								<tr>
									<th className="text-left px-4 py-2 font-medium">Time</th>
									<th className="text-left px-4 py-2 font-medium">Type</th>
									<th className="text-left px-4 py-2 font-medium">
										Broadcaster
									</th>
									<th className="text-left px-4 py-2 font-medium">Status</th>
									<th className="text-left px-4 py-2 font-medium">Duration</th>
									<th className="text-left px-4 py-2 font-medium">Error</th>
								</tr>
							</thead>
							<tbody>
								{logs.map((log) => (
									<tr
										key={log.id}
										className="border-t border-border hover:bg-muted/30"
									>
										<td className="px-4 py-2 whitespace-nowrap text-muted-foreground">
											{new Date(log.fetched_at).toLocaleString()}
										</td>
										<td className="px-4 py-2">{log.fetch_type}</td>
										<td className="px-4 py-2 text-muted-foreground">
											{log.broadcaster_id || "—"}
										</td>
										<td className="px-4 py-2">
											<span
												className={
													log.status >= 200 && log.status < 300
														? "text-emerald-600 dark:text-emerald-400"
														: "text-destructive"
												}
											>
												{log.status}
											</span>
										</td>
										<td className="px-4 py-2 text-muted-foreground">
											{log.duration_ms}ms
										</td>
										<td className="px-4 py-2 text-destructive truncate max-w-xs">
											{log.error || ""}
										</td>
									</tr>
								))}
							</tbody>
						</table>
					</div>

					<div className="flex items-center gap-2 mt-4">
						<button
							type="button"
							disabled={page === 0}
							onClick={() => setPage((p) => Math.max(0, p - 1))}
							className="px-3 py-1 rounded-md border border-border disabled:opacity-50"
						>
							Previous
						</button>
						<span className="text-sm text-muted-foreground">
							Page {page + 1} of {pageCount || 1} ({total} total)
						</span>
						<button
							type="button"
							disabled={page >= pageCount - 1}
							onClick={() => setPage((p) => p + 1)}
							className="px-3 py-1 rounded-md border border-border disabled:opacity-50"
						>
							Next
						</button>
					</div>
				</>
			)}
		</div>
	)
}
