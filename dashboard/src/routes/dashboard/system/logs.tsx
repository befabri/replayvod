import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { TitledLayout } from "@/components/layout/titled-layout";
import { DataTable } from "@/components/ui/data-table";
import { useFetchLogs } from "@/features/system";
import { fetchLogColumns } from "@/features/system/components/logColumns";

const PAGE_SIZE = 50;

export const Route = createFileRoute("/dashboard/system/logs")({
	component: SystemLogsPage,
});

function SystemLogsPage() {
	const [page, setPage] = useState(0);
	const { data, isLoading, error } = useFetchLogs(PAGE_SIZE, page * PAGE_SIZE);

	const total = data?.total ?? 0;
	const logs = data?.data ?? [];
	const pageCount = Math.ceil(total / PAGE_SIZE);

	return (
		<TitledLayout title="API Fetch Logs">
			{isLoading && <div className="text-muted-foreground">Loading…</div>}

			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					Failed to load logs: {error.message}
				</div>
			)}

			{!isLoading && !error && (
				<>
					<DataTable
						columns={fetchLogColumns}
						data={logs}
						emptyMessage="No fetch logs yet."
					/>

					{logs.length > 0 && (
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
					)}
				</>
			)}
		</TitledLayout>
	);
}
