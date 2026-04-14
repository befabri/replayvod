import { createFileRoute } from "@tanstack/react-router";
import { TitledLayout } from "@/components/layout/titled-layout";
import { DataTable } from "@/components/ui/data-table";
import { useSessions } from "@/features/sessions";
import { sessionColumns } from "@/features/sessions/components/columns";

export const Route = createFileRoute("/dashboard/sessions")({
	component: SessionsPage,
});

function SessionsPage() {
	const { data, isLoading, error } = useSessions();

	return (
		<TitledLayout title="Active sessions">
			<p className="text-muted-foreground mb-6 -mt-6">
				Every session currently signed into this account. Revoke any that don't
				belong to a device you recognize.
			</p>

			{isLoading && <div className="text-muted-foreground">Loading…</div>}
			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					Failed to load sessions: {error.message}
				</div>
			)}

			{data && (
				<DataTable
					columns={sessionColumns}
					data={data}
					emptyMessage="No active sessions."
				/>
			)}
		</TitledLayout>
	);
}
