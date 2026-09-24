import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { BadgeSkeleton, Skeleton } from "@/components/ui/skeleton";
import type { TaskResponse } from "@/features/tasks";
import { StatusBadge } from "./StatusBadge";
import { TaskActions } from "./TaskActions";

export function taskColumns(t: TFunction): ColumnDef<TaskResponse>[] {
	return [
		{
			accessorKey: "name",
			header: t("tasks.col_name"),
			enableSorting: true,
			meta: {
				skeleton: (
					<div>
						<div className="flex h-4 items-center">
							<Skeleton className="h-3 w-32" />
						</div>
						<div className="mt-0.5 flex h-4 items-center">
							<Skeleton className="h-3 w-72 max-w-full" />
						</div>
						<div className="mt-1 flex h-4 items-center">
							<Skeleton className="h-3 w-24" />
						</div>
					</div>
				),
			},
			cell: ({ row }) => {
				const task = row.original;
				return (
					<div>
						<div className="font-mono text-xs">{task.name}</div>
						<div className="text-xs text-muted-foreground mt-0.5 max-w-md">
							{task.description}
						</div>
						<div className="text-xs text-muted-foreground mt-1">
							{!task.is_available && <p>{t("tasks.unavailable")}</p>}
							{t("tasks.interval")}:{" "}
							{task.interval_seconds > 0
								? `${task.interval_seconds}s`
								: t("tasks.manual_only")}
						</div>
					</div>
				);
			},
		},
		{
			accessorKey: "last_status",
			header: t("tasks.col_status"),
			enableSorting: true,
			meta: { skeleton: <BadgeSkeleton className="w-16" /> },
			cell: ({ row }) => {
				const task = row.original;
				return (
					<div>
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
					</div>
				);
			},
		},
		{
			accessorKey: "last_run_at",
			header: t("tasks.col_last_run"),
			enableSorting: true,
			cell: ({ row }) => {
				const task = row.original;
				return (
					<span className="text-xs text-muted-foreground">
						{task.last_run_at
							? new Date(task.last_run_at).toLocaleString()
							: t("tasks.never")}
					</span>
				);
			},
		},
		{
			accessorKey: "next_run_at",
			header: t("tasks.col_next_run"),
			enableSorting: true,
			cell: ({ row }) => {
				const task = row.original;
				return (
					<span className="text-xs text-muted-foreground">
						{task.next_run_at
							? new Date(task.next_run_at).toLocaleString()
							: "—"}
					</span>
				);
			},
		},
		{
			id: "actions",
			header: () => (
				<span className="block w-full text-right">{t("common.actions")}</span>
			),
			enableSorting: false,
			meta: {
				skeleton: (
					<div className="flex flex-col items-end gap-1">
						<Skeleton className="h-6 w-18" />
						<Skeleton className="h-6 w-14" />
					</div>
				),
			},
			cell: ({ row }) => <TaskActions task={row.original} />,
		},
	];
}
