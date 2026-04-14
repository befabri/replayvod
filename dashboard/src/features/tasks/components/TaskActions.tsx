import { useTranslation } from "react-i18next";
import type { TaskResponse } from "@/features/tasks";
import { useRunTaskNow, useToggleTask } from "@/features/tasks";

export function TaskActions({ task }: { task: TaskResponse }) {
	const { t } = useTranslation();
	const toggle = useToggleTask();
	const runNow = useRunTaskNow();
	return (
		<div className="flex flex-col items-end gap-1">
			<button
				type="button"
				onClick={() => runNow.mutate({ name: task.name })}
				disabled={runNow.isPending}
				className="text-xs px-2 py-1 rounded-md border border-border hover:bg-muted disabled:opacity-60"
			>
				{t("tasks.run_now")}
			</button>
			<button
				type="button"
				onClick={() =>
					toggle.mutate({ name: task.name, enabled: !task.is_enabled })
				}
				disabled={toggle.isPending}
				className="text-xs text-muted-foreground hover:text-foreground disabled:opacity-60"
			>
				{task.is_enabled ? t("tasks.pause") : t("tasks.resume")}
			</button>
		</div>
	);
}
