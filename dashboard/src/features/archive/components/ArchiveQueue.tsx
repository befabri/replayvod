import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { createContext, type ReactNode, useContext, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import type {
	ActiveDownloadResponse,
	StorageState,
	VideoResponse,
} from "@/api/generated/trpc";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { DataTable } from "@/components/ui/data-table";
import { TimestampValue } from "@/components/ui/timestamp";
import {
	useArchiveQueue,
	useCancelArchiveRetry,
	useDequeueArchive,
	useLiveArchiveQueue,
	useRetryArchive,
} from "@/features/archive/queries";
import {
	storageUnwritable,
	useStorageStatus,
} from "@/features/storage/queries";
import { RecordingCell } from "@/features/videos/components/activityColumns";
import { VideoStatusBadge } from "@/features/videos/components/VideoStatusBadge";
import { useCanManageVideos } from "@/features/videos/permissions";
import {
	useCancelDownload,
	useDownloadCapacity,
	useLiveActiveDownloads,
} from "@/features/videos/queries";
import { formatAbsolute, useUntil } from "@/lib/format-relative";

const ArchiveStorageContext = createContext<StorageState | undefined>(
	undefined,
);

const EMPTY_PROGRESS: ReadonlyMap<string, ActiveDownloadResponse> = new Map();
const ArchiveProgressContext = createContext(EMPTY_PROGRESS);

function LiveArchiveProgress({ children }: { children: ReactNode }) {
	const { data: active } = useLiveActiveDownloads();
	const { data: storage } = useStorageStatus();
	useLiveArchiveQueue();
	const progressByJob = useMemo(() => {
		const map = new Map<string, ActiveDownloadResponse>();
		for (const row of active ?? []) map.set(row.video.job_id, row);
		return map;
	}, [active]);
	return (
		<ArchiveStorageContext.Provider value={storage?.state}>
			<ArchiveProgressContext.Provider value={progressByJob}>
				{children}
			</ArchiveProgressContext.Provider>
		</ArchiveStorageContext.Provider>
	);
}

export function ArchiveQueue() {
	return (
		<LiveArchiveProgress>
			<ArchiveQueueTables />
		</LiveArchiveProgress>
	);
}

function ArchiveQueueTables() {
	const { t, i18n } = useTranslation();
	const queue = useArchiveQueue();
	const { data: capacity } = useDownloadCapacity();
	const canManage = useCanManageVideos();
	const rows = queue.data?.queue ?? [];
	const failures = queue.data?.failures ?? [];
	const columns = useMemo(
		() => queueColumns(t, i18n.language, canManage),
		[t, i18n.language, canManage],
	);
	const failureCols = useMemo(
		() => failureColumns(t, i18n.language, canManage),
		[t, i18n.language, canManage],
	);

	return (
		<div className="space-y-10">
			<section className="space-y-4">
				<div className="flex flex-wrap items-end justify-between gap-3">
					<div>
						<h2 className="text-lg font-medium tracking-tight">
							{t("archive.queue_title")}
						</h2>
						<p className="text-sm text-muted-foreground">
							{t("archive.queue_description")}
						</p>
					</div>
					{capacity ? (
						<div className="font-mono text-xs tracking-[0.16em] text-muted-foreground uppercase">
							{t("archive.queue_capacity", {
								count: capacity.archive_max_concurrent,
							})}
						</div>
					) : null}
				</div>
				{queue.isError ? (
					<Alert variant="destructive">
						{t("archive.queue_failed")}: {queue.error.message}
					</Alert>
				) : (
					<DataTable
						columns={columns}
						data={rows}
						loading={queue.isLoading}
						emptyMessage={t("archive.queue_empty")}
					/>
				)}
			</section>
			{queue.data ? (
				<section className="space-y-4" data-testid="archive-failures">
					<div>
						<h2 className="text-lg font-medium tracking-tight">
							{t("archive.failures_title")}
						</h2>
						<p className="text-sm text-muted-foreground">
							{t("archive.failures_description")}
						</p>
					</div>
					<DataTable
						columns={failureCols}
						data={failures}
						emptyMessage={t("archive.failures_empty")}
					/>
				</section>
			) : null}
		</div>
	);
}

function queueColumns(
	t: TFunction,
	locale: string,
	canManage: boolean,
): ColumnDef<VideoResponse>[] {
	const columns: ColumnDef<VideoResponse>[] = [
		{
			accessorKey: "display_name",
			header: t("archive.col_vod"),
			cell: ({ row }) => <RecordingCell row={row.original} t={t} />,
		},
		{
			accessorKey: "broadcast_at",
			header: t("archive.col_streamed"),
			cell: ({ row }) => (
				<span className="whitespace-nowrap text-sm tabular-nums">
					{row.original.broadcast_at
						? formatAbsolute(row.original.broadcast_at, locale)
						: "—"}
				</span>
			),
		},
		{
			accessorKey: "status",
			header: t("archive.col_status"),
			cell: ({ row }) => (
				<VideoStatusBadge
					status={row.original.status}
					completionKind={row.original.completion_kind}
					t={t}
				/>
			),
		},
		{
			id: "progress",
			header: t("archive.col_progress"),
			cell: ({ row }) => <ProgressCell row={row.original} t={t} />,
		},
		{
			accessorKey: "start_download_at",
			header: t("archive.col_queued"),
			cell: ({ row }) => (
				<TimestampValue
					iso={row.original.start_download_at}
					locale={locale}
					className="whitespace-nowrap text-sm"
				/>
			),
		},
	];
	if (canManage) {
		columns.push({
			id: "actions",
			header: () => <span className="sr-only">{t("common.actions")}</span>,
			cell: ({ row }) => <QueueActions row={row.original} />,
		});
	}
	return columns;
}

function failureColumns(
	t: TFunction,
	locale: string,
	canManage: boolean,
): ColumnDef<VideoResponse>[] {
	const columns: ColumnDef<VideoResponse>[] = [
		{
			accessorKey: "display_name",
			header: t("archive.col_vod"),
			cell: ({ row }) => <RecordingCell row={row.original} t={t} />,
		},
		{
			accessorKey: "status",
			header: t("archive.col_status"),
			cell: ({ row }) => (
				<VideoStatusBadge
					status={row.original.status}
					completionKind={row.original.completion_kind}
					t={t}
				/>
			),
		},
		{
			accessorKey: "error",
			header: t("archive.col_error"),
			cell: ({ row }) => (
				<span
					className="block max-w-sm truncate text-sm text-muted-foreground"
					title={row.original.error ?? undefined}
				>
					{row.original.error ?? "—"}
				</span>
			),
		},
		{
			accessorKey: "next_retry_at",
			header: t("archive.col_retry"),
			cell: ({ row }) => <RetryCell row={row.original} t={t} />,
		},
		{
			accessorKey: "downloaded_at",
			header: t("archive.col_failed_at"),
			cell: ({ row }) =>
				row.original.downloaded_at ? (
					<TimestampValue
						iso={row.original.downloaded_at}
						locale={locale}
						className="whitespace-nowrap text-sm"
					/>
				) : (
					<span className="text-muted-foreground">—</span>
				),
		},
	];
	if (canManage) {
		columns.push({
			id: "actions",
			header: () => <span className="sr-only">{t("common.actions")}</span>,
			cell: ({ row }) => <FailureActions row={row.original} />,
		});
	}
	return columns;
}

function RetryCell({ row, t }: { row: VideoResponse; t: TFunction }) {
	const until = useUntil(row.next_retry_at);
	if (!until) {
		return (
			<span className="text-sm text-muted-foreground">
				{t("archive.no_retry")}
			</span>
		);
	}
	return (
		<span className="whitespace-nowrap text-sm" data-testid="archive-retry-in">
			{t("archive.retry_in", { when: until })}
		</span>
	);
}

function QueueActions({ row }: { row: VideoResponse }) {
	const { t } = useTranslation();
	const cancel = useCancelDownload();
	const dequeue = useDequeueArchive();

	if (row.status === "RUNNING") {
		return (
			<Button
				type="button"
				variant="ghost"
				size="sm"
				className="text-destructive"
				disabled={cancel.isPending}
				onClick={() => cancel.mutate({ job_id: row.job_id })}
			>
				{t("archive.cancel")}
			</Button>
		);
	}
	if (row.status === "PENDING") {
		return (
			<Button
				type="button"
				variant="ghost"
				size="sm"
				disabled={dequeue.isPending}
				onClick={() =>
					dequeue.mutate(
						{ video_id: row.id },
						{
							onSuccess: () => toast.success(t("archive.removed")),
							onError: (err) =>
								toast.error(err.message || t("archive.remove_failed")),
						},
					)
				}
			>
				{t("archive.remove")}
			</Button>
		);
	}
	return null;
}

function FailureActions({ row }: { row: VideoResponse }) {
	const { t } = useTranslation();
	const retry = useRetryArchive();
	const cancelRetry = useCancelArchiveRetry();
	return (
		<div className="flex items-center justify-end gap-1">
			<Button
				type="button"
				variant="ghost"
				size="sm"
				disabled={retry.isPending}
				onClick={() =>
					retry.mutate(
						{ video_id: row.id },
						{
							onSuccess: () => toast.success(t("archive.retried")),
							onError: (err) =>
								toast.error(err.message || t("archive.retry_failed")),
						},
					)
				}
			>
				{t("archive.retry_now")}
			</Button>
			{row.next_retry_at ? (
				<Button
					type="button"
					variant="ghost"
					size="sm"
					className="text-muted-foreground"
					disabled={cancelRetry.isPending}
					onClick={() =>
						cancelRetry.mutate(
							{ video_id: row.id },
							{
								onSuccess: () => toast.success(t("archive.retry_cancelled")),
								onError: (err) =>
									toast.error(err.message || t("archive.cancel_retry_failed")),
							},
						)
					}
				>
					{t("archive.cancel_retry")}
				</Button>
			) : null}
		</div>
	);
}

function ProgressCell({ row, t }: { row: VideoResponse; t: TFunction }) {
	const progress = useContext(ArchiveProgressContext).get(row.job_id);
	const storageState = useContext(ArchiveStorageContext);
	if (row.status === "PENDING") {
		return (
			<span className="text-sm text-muted-foreground">
				{t(
					storageUnwritable(storageState)
						? "archive.waiting_storage"
						: "archive.waiting",
				)}
			</span>
		);
	}
	if (row.status !== "RUNNING") {
		return <span className="text-muted-foreground">—</span>;
	}
	if (!progress || progress.percent < 0) {
		return (
			<span className="text-sm text-muted-foreground">
				{progress?.speed || t("archive.progress_starting")}
			</span>
		);
	}
	const percent = Math.min(100, Math.max(0, progress.percent));
	const details = [
		progress.speed,
		progress.eta ? t("archive.eta_left", { eta: progress.eta }) : "",
	].filter(Boolean);
	return (
		<div className="min-w-40 space-y-1">
			<div className="flex items-center justify-between gap-2 font-mono text-xs tabular-nums">
				<span className="text-foreground">{Math.round(percent)}%</span>
				<span className="text-muted-foreground">{details.join(" · ")}</span>
			</div>
			<div
				className="h-1.5 overflow-hidden rounded-full bg-muted/50"
				role="progressbar"
				aria-valuemin={0}
				aria-valuemax={100}
				aria-valuenow={Math.round(percent)}
				aria-label={t("archive.col_progress")}
			>
				<div
					className="h-full rounded-full bg-primary/80"
					style={{ width: `${Math.max(percent, 2)}%` }}
				/>
			</div>
		</div>
	);
}
