import { Link } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { useState } from "react";
import { TimestampValue } from "@/components/ui/timestamp";
import { channelLabel, type VideoResponse } from "@/features/videos";
import { formatBytes } from "@/features/videos/format";
import { cn } from "@/lib/utils";
import { VideoThumbnail } from "./listColumns";
import { VideoRemovalActions } from "./VideoRemovalActions";
import { VideoStatusBadge } from "./VideoStatusBadge";

export type HistoryOutcome = "all" | "failed" | "cancelled";

export type HistoryMedia = "any" | "on_disk" | "removed" | "unavailable";

export type HistoryView = {
	outcome: HistoryOutcome;
	media: HistoryMedia;
};

function channelColumn(t: TFunction): ColumnDef<VideoResponse> {
	return {
		accessorKey: "display_name",
		header: t("history.col_recording"),
		cell: ({ row }) => <RecordingCell row={row.original} t={t} />,
	};
}

export function RecordingCell({
	row,
	t,
}: {
	row: VideoResponse;
	t: TFunction;
}) {
	const title = row.title?.trim();
	const subtitle = title && title !== channelLabel(row) ? title : null;
	const watchable = !row.deleted_at && row.status === "DONE";
	return (
		<div
			className={cn(
				"flex min-w-0 items-center gap-3",
				row.deleted_at && "opacity-50",
			)}
		>
			<PosterCell
				row={row}
				label={subtitle ?? channelLabel(row)}
				watchable={watchable}
				t={t}
			/>
			<div className="min-w-0">
				<ChannelCell row={row} />
				{subtitle ? (
					<TitleCell row={row} title={subtitle} watchable={watchable} />
				) : null}
			</div>
		</div>
	);
}

const FOCUS_RING =
	"focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2";

function PosterCell({
	row,
	label,
	watchable,
	t,
}: {
	row: VideoResponse;
	label: string;
	watchable: boolean;
	t: TFunction;
}) {
	const poster = <VideoThumbnail video={row} t={t} />;
	if (!watchable) {
		return poster;
	}
	return (
		<Link
			to="/dashboard/watch/$videoId"
			params={{ videoId: String(row.id) }}
			search={{ t: undefined }}
			className={cn("block shrink-0 rounded-md", FOCUS_RING)}
			aria-label={t("videos.watch_recording", { title: label })}
		>
			{poster}
		</Link>
	);
}

function TitleCell({
	row,
	title,
	watchable,
}: {
	row: VideoResponse;
	title: string;
	watchable: boolean;
}) {
	const className = "block max-w-xs truncate text-xs text-muted-foreground";
	if (!watchable) {
		return (
			<span className={className} title={title}>
				{title}
			</span>
		);
	}
	return (
		<Link
			to="/dashboard/watch/$videoId"
			params={{ videoId: String(row.id) }}
			search={{ t: undefined }}
			className={cn(className, "transition-colors hover:text-link", FOCUS_RING)}
			title={title}
		>
			{title}
		</Link>
	);
}

function ChannelCell({ row }: { row: VideoResponse }) {
	const label = channelLabel(row);
	if (!row.broadcaster_id) {
		return <span className="font-medium">{label}</span>;
	}
	return (
		<Link
			to="/dashboard/channels/$channelId"
			params={{ channelId: row.broadcaster_id }}
			className="font-medium hover:text-link"
		>
			{label}
		</Link>
	);
}

function statusColumn(t: TFunction): ColumnDef<VideoResponse> {
	return {
		accessorKey: "status",
		header: t("history.col_status"),
		cell: ({ row }) => (
			<VideoStatusBadge
				status={row.original.status}
				completionKind={row.original.completion_kind}
				t={t}
			/>
		),
	};
}

const mediaColumn = (t: TFunction): ColumnDef<VideoResponse> => ({
	id: "media",
	header: t("history.col_media"),
	cell: ({ row }) => <MediaCell row={row.original} t={t} />,
});

export function MediaCell({ row, t }: { row: VideoResponse; t: TFunction }) {
	if (row.deleted_at) {
		return (
			<span
				className="text-muted-foreground"
				title={
					row.deletion_kind === "missing"
						? t("history.media_missing_hint")
						: undefined
				}
			>
				{t(mediaLabelKey(row.deletion_kind))}
			</span>
		);
	}
	if (row.status !== "DONE" && !row.has_media) {
		return <span className="text-muted-foreground">—</span>;
	}
	return <span>{t("history.media_present")}</span>;
}

function mediaLabelKey(deletionKind?: string | null): string {
	switch (deletionKind) {
		case "retention":
			return "history.media_retention";
		case "missing":
			return "history.media_missing";
		default:
			return "history.media_manual";
	}
}

const dimmedIfRemoved = (row: VideoResponse) =>
	row.deleted_at ? "opacity-50" : undefined;

const qualityColumn = (t: TFunction): ColumnDef<VideoResponse> => ({
	accessorKey: "quality",
	header: t("history.col_quality"),
	cell: ({ row }) => (
		<span className={dimmedIfRemoved(row.original)}>
			{row.original.quality}
		</span>
	),
});

const sizeColumn = (t: TFunction): ColumnDef<VideoResponse> => ({
	accessorKey: "size_bytes",
	header: t("history.col_size"),
	cell: ({ row }) => (
		<span
			className={cn(
				"text-xs text-muted-foreground",
				dimmedIfRemoved(row.original),
			)}
		>
			{formatBytes(row.original.size_bytes)}
		</span>
	),
});

const whenColumn = (
	t: TFunction,
	locale: string,
): ColumnDef<VideoResponse> => ({
	id: "when",
	accessorFn: (row) =>
		row.deleted_at ?? row.downloaded_at ?? row.start_download_at,
	header: t("history.col_when"),
	cell: ({ row }) => {
		const v = row.original;
		return (
			<TimestampValue
				iso={v.deleted_at ?? v.downloaded_at ?? v.start_download_at}
				locale={locale}
				className="text-xs text-muted-foreground"
			/>
		);
	},
});

const errorColumn = (t: TFunction): ColumnDef<VideoResponse> => ({
	accessorKey: "error",
	header: t("history.col_error"),
	cell: ({ row }) => <ErrorCell error={row.original.error} />,
});

function actionsColumn(canManage: boolean): ColumnDef<VideoResponse> {
	return {
		id: "actions",
		header: "",
		cell: ({ row }) => {
			if (!canManage) return null;
			return (
				<div className="flex items-center justify-end gap-2">
					<VideoRemovalActions video={row.original} />
				</div>
			);
		},
	};
}

function ErrorCell({ error }: { error?: string }) {
	const [expanded, setExpanded] = useState(false);
	if (!error) {
		return <span className="text-xs text-muted-foreground">—</span>;
	}
	return (
		<button
			type="button"
			onClick={() => setExpanded((e) => !e)}
			title={expanded ? undefined : error}
			className={cn(
				"max-w-md text-left text-xs text-destructive hover:underline",
				!expanded && "line-clamp-1",
			)}
		>
			{error}
		</button>
	);
}

export function historyColumns(
	t: TFunction,
	view: HistoryView,
	canManage: boolean,
	locale: string,
): ColumnDef<VideoResponse>[] {
	const cols: ColumnDef<VideoResponse>[] = [channelColumn(t), statusColumn(t)];
	if (view.media !== "on_disk") {
		cols.push(mediaColumn(t));
	}
	cols.push(qualityColumn(t));
	if (view.outcome !== "failed") {
		cols.push(sizeColumn(t));
	}
	cols.push(whenColumn(t, locale));
	if (view.outcome === "failed") {
		cols.push(errorColumn(t));
	}
	cols.push(actionsColumn(canManage));
	return cols.map((col) => {
		const id =
			"accessorKey" in col && col.accessorKey
				? String(col.accessorKey)
				: col.id;
		return {
			...col,
			enableSorting: id !== undefined && id in HISTORY_SORT_BY_COLUMN,
		};
	});
}

export const HISTORY_SORT_BY_COLUMN: Record<
	string,
	"channel" | "size" | "history_when"
> = {
	display_name: "channel",
	size_bytes: "size",
	when: "history_when",
};
