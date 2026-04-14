import { createFileRoute } from "@tanstack/react-router";
import { TitledLayout } from "@/components/layout/titled-layout";
import { DataTable } from "@/components/ui/data-table";
import { useVideos } from "@/features/videos";
import { queueColumns } from "@/features/videos/components/activityColumns";

export const Route = createFileRoute("/dashboard/activity/queue")({
	component: QueuePage,
});

function QueuePage() {
	const running = useVideos(50, 0, "RUNNING");
	const pending = useVideos(50, 0, "PENDING");

	const rows = [...(running.data ?? []), ...(pending.data ?? [])];
	const loading = running.isLoading || pending.isLoading;
	const error = running.error ?? pending.error;

	return (
		<TitledLayout title="Download queue">
			<p className="text-muted-foreground mb-6 -mt-6">
				Downloads currently running or waiting to start.
			</p>

			{loading && <div className="text-muted-foreground">Loading…</div>}
			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					Failed to load queue: {error.message}
				</div>
			)}
			{!loading && !error && (
				<DataTable
					columns={queueColumns}
					data={rows}
					emptyMessage="No downloads in progress."
				/>
			)}
		</TitledLayout>
	);
}
