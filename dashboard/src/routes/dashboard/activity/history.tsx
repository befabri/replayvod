import { createFileRoute } from "@tanstack/react-router"
import { useState } from "react"
import { DataTable } from "@/components/ui/data-table"
import { useVideos } from "@/features/videos"
import { historyColumns } from "@/features/videos/components/activityColumns"

const PAGE_SIZE = 50

export const Route = createFileRoute("/dashboard/activity/history")({
	validateSearch: (search: Record<string, unknown>) => ({
		status:
			search.status === "FAILED" || search.status === "DONE"
				? (search.status as "DONE" | "FAILED")
				: ("DONE" as const),
	}),
	component: HistoryPage,
})

function HistoryPage() {
	const { status } = Route.useSearch()
	const navigate = Route.useNavigate()
	const [page, setPage] = useState(0)
	const { data, isLoading, error } = useVideos(
		PAGE_SIZE,
		page * PAGE_SIZE,
		status,
	)

	return (
		<div className="p-8 max-w-5xl">
			<h1 className="text-3xl font-heading font-bold mb-2">Download history</h1>
			<p className="text-sm text-muted-foreground mb-6">
				Completed and failed downloads. Filter by status to investigate
				failures.
			</p>

			<div className="flex gap-1 mb-4">
				{(["DONE", "FAILED"] as const).map((opt) => (
					<button
						key={opt}
						type="button"
						onClick={() => {
							setPage(0)
							void navigate({ search: { status: opt } })
						}}
						className={`px-3 py-1 rounded-md text-sm border transition-colors ${
							status === opt
								? "bg-primary text-primary-foreground border-primary"
								: "border-border hover:bg-muted"
						}`}
					>
						{opt}
					</button>
				))}
			</div>

			{isLoading && <div className="text-muted-foreground">Loading…</div>}
			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					Failed to load history: {error.message}
				</div>
			)}
			{!isLoading && !error && (
				<>
					<DataTable
						columns={historyColumns}
						data={data ?? []}
						emptyMessage="No entries."
					/>
					<div className="flex items-center gap-2 mt-4">
						<button
							type="button"
							disabled={page === 0}
							onClick={() => setPage((p) => Math.max(0, p - 1))}
							className="px-3 py-1 rounded-md border border-border disabled:opacity-50 text-sm"
						>
							Previous
						</button>
						<span className="text-sm text-muted-foreground">
							Page {page + 1}
						</span>
						<button
							type="button"
							disabled={(data?.length ?? 0) < PAGE_SIZE}
							onClick={() => setPage((p) => p + 1)}
							className="px-3 py-1 rounded-md border border-border disabled:opacity-50 text-sm"
						>
							Next
						</button>
					</div>
				</>
			)}
		</div>
	)
}
