import { createFileRoute } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import type { TaskResponse } from "@/features/tasks"
import {
	useRunTaskNow,
	useTasks,
	useToggleTask,
} from "@/features/tasks"

export const Route = createFileRoute("/dashboard/system/tasks")({
	component: TasksPage,
})

function TasksPage() {
	const { t } = useTranslation()
	const { data, isLoading, error } = useTasks()

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

function TaskRow({ task }: { task: TaskResponse }) {
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

function StatusBadge({
	status,
	enabled,
	error,
}: {
	status: string
	enabled: boolean
	error?: string
}) {
	const { t } = useTranslation()
	if (!enabled) {
		return (
			<span className="inline-flex items-center rounded-md px-2 py-0.5 text-xs bg-muted text-muted-foreground">
				{t("tasks.status_paused")}
			</span>
		)
	}
	const variant = {
		success: "bg-primary/20 text-primary-foreground",
		failed: "bg-destructive/20 text-destructive",
		running: "bg-primary/20 text-primary-foreground animate-pulse",
		pending: "bg-muted text-muted-foreground",
		skipped: "bg-muted text-muted-foreground",
	}[status] ?? "bg-muted text-muted-foreground"
	return (
		<>
			<span
				className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs ${variant}`}
			>
				{t(`tasks.status_${status}`, { defaultValue: status })}
			</span>
			{error && (
				<div className="text-xs text-destructive mt-1 max-w-sm truncate" title={error}>
					{error}
				</div>
			)}
		</>
	)
}
