import { createFileRoute } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import { TaskRow } from "@/features/tasks/components/TaskRow"
import { useLiveTaskStatus, useTasks } from "@/features/tasks"

export const Route = createFileRoute("/dashboard/system/tasks")({
	component: TasksPage,
})

function TasksPage() {
	const { t } = useTranslation()
	const { data, isLoading, error } = useTasks()
	// Mount the task.status SSE subscription — each lifecycle transition
	// triggers a task.list re-fetch so the UI reflects running →
	// success / failed without polling.
	useLiveTaskStatus()

	return (
		<div className="p-8 max-w-5xl">
			<h1 className="text-3xl font-heading font-bold mb-2">
				{t("tasks.title")}
			</h1>
			<p className="text-sm text-muted-foreground mb-6">
				{t("tasks.description")}
			</p>

			{isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{t("tasks.failed_to_load")}: {error.message}
				</div>
			)}
			{data && data.data.length === 0 && (
				<div className="text-muted-foreground">{t("tasks.empty")}</div>
			)}

			{data && data.data.length > 0 && (
				<div className="rounded-lg border border-border overflow-hidden">
					<table className="w-full text-sm">
						<thead className="bg-muted/50">
							<tr>
								<th className="text-left px-3 py-2 font-medium">
									{t("tasks.col_name")}
								</th>
								<th className="text-left px-3 py-2 font-medium">
									{t("tasks.col_status")}
								</th>
								<th className="text-left px-3 py-2 font-medium">
									{t("tasks.col_last_run")}
								</th>
								<th className="text-left px-3 py-2 font-medium">
									{t("tasks.col_next_run")}
								</th>
								<th className="text-right px-3 py-2 font-medium" />
							</tr>
						</thead>
						<tbody>
							{data.data.map((task) => (
								<TaskRow key={task.name} task={task} />
							))}
						</tbody>
					</table>
				</div>
			)}
		</div>
	)
}
