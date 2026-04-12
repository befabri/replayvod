import { createFileRoute } from "@tanstack/react-router"
import { useMemo } from "react"
import { useTranslation } from "react-i18next"
import { DataTable } from "@/components/ui/data-table"
import { taskColumns } from "@/features/tasks/components/columns"
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

	const columns = useMemo(() => taskColumns(t), [t])

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

			{data && (
				<DataTable
					columns={columns}
					data={data.data}
					emptyMessage={t("tasks.empty")}
				/>
			)}
		</div>
	)
}
