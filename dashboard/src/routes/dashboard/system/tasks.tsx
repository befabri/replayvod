import { createFileRoute } from "@tanstack/react-router";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { QueryTable } from "@/components/ui/query-table";
import { useLiveTaskStatus, useTasks } from "@/features/tasks";
import { taskColumns } from "@/features/tasks/components/columns";

export const Route = createFileRoute("/dashboard/system/tasks")({
	component: TasksPage,
});

function TasksPage() {
	const { t } = useTranslation();
	const tasks = useTasks();
	// Mount the task.status SSE subscription — each lifecycle transition
	// triggers a task.list re-fetch so the UI reflects running →
	// success / failed without polling.
	useLiveTaskStatus();

	const columns = useMemo(() => taskColumns(t), [t]);

	return (
		<TitledLayout title={t("tasks.title")}>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("tasks.description")}
			</p>

			<QueryTable
				query={tasks}
				columns={columns}
				getRows={(data) => data.data}
				emptyMessage={t("tasks.empty")}
				errorLabel={t("tasks.failed_to_load")}
			/>
		</TitledLayout>
	);
}
