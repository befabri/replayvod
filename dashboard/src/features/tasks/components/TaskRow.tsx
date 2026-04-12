import { useTranslation } from "react-i18next"
import type { TaskResponse } from "@/features/tasks"
import { useRunTaskNow, useToggleTask } from "@/features/tasks"
import { StatusBadge } from "./StatusBadge"

export function TaskRow({ task }: { task: TaskResponse }) {
	const { t } = useTranslation()
	const toggle = useToggleTask()
	const runNow = useRunTaskNow()
	return (
		<tr className="border-t border-border align-top">
			<td className="px-3 py-3">
				<div className="font-mono text-xs">{task.name}</div>
				<div className="text-xs text-muted-foreground mt-0.5 max-w-md">
					{task.description}
				</div>
				<div className="text-xs text-muted-foreground mt-1">
					{t("tasks.interval")}:{" "}
					{task.interval_seconds > 0
						? `${task.interval_seconds}s`
						: t("tasks.manual_only")}
				</div>
			</td>
			<td className="px-3 py-3">
				<StatusBadge
					status={task.last_status}
					enabled={task.is_enabled}
					error={task.last_error ?? undefined}
				/>
				{task.last_duration_ms > 0 && (
					<div className="text-xs text-muted-foreground mt-1">
						{task.last_duration_ms}ms
					</div>
				)}
			</td>
			<td className="px-3 py-3 text-xs text-muted-foreground">
				{task.last_run_at
					? new Date(task.last_run_at).toLocaleString()
					: t("tasks.never")}
			</td>
			<td className="px-3 py-3 text-xs text-muted-foreground">
				{task.next_run_at
					? new Date(task.next_run_at).toLocaleString()
					: "—"}
			</td>
			<td className="px-3 py-3 text-right space-y-1">
				<button
					type="button"
					onClick={() => runNow.mutate({ name: task.name })}
					disabled={runNow.isPending}
					className="text-xs px-2 py-1 rounded-md border border-border hover:bg-muted disabled:opacity-60"
				>
					{t("tasks.run_now")}
				</button>
				<br />
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
			</td>
		</tr>
	)
}
