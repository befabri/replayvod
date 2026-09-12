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

// HistoryOutcome is the download outcome the tabs filter on, and it is the same
// axis as the Status column. The server owns what separates a failure from a
// run the operator stopped, so "cancelled" is a value here rather than a rule
// the dashboard has to know.
export type HistoryOutcome = "all" | "failed" | "cancelled";

// HistoryMedia is the other axis, the same one the Media column shows: whether
// the recording's files are still on disk.
export type HistoryMedia = "any" | "on_disk" | "removed" | "unavailable";

// HistoryView is one cell of the two axes, which together are what the page's
// two controls select.
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

// RecordingCell identifies a row at a glance: the poster while one is still in
// storage (a missing-media tombstone keeps it, every other removal purges it),
// the channel link, and the stream title underneath. Poster and title both open
// the player, so the row needs no Watch button of its own.
export function RecordingCell({
	row,
	t,
}: {
	row: VideoResponse;
	t: TFunction;
}) {
	const title = row.title?.trim();
	const subtitle = title && title !== channelLabel(row) ? title : null;
	// Only a finished recording that still has its media opens the player; a
	// tombstone or a failure has nothing to play, so it stays plain content.
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

// ChannelCell links the channel name to its channel page (broadcaster_id), the
// same target the video cards use. Falls back to plain text for the rare row
// with no synced broadcaster.
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

// statusColumn owns one axis: what the recorder did. Whether the media survived
// is the media column's business, so a tombstone never adds a second badge here.
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

// mediaColumn owns the other axis: whether the recording's files are still on
// disk, and why they left if they aren't. Plain text rather than a badge, so it
// never competes with the status pill next to it.
const mediaColumn = (t: TFunction): ColumnDef<VideoResponse> => ({
	id: "media",
	header: t("history.col_media"),
	cell: ({ row }) => <MediaCell row={row.original} t={t} />,
});

// MediaCell reads deletion_kind to tell auto-cleanup (retention), media that
// vanished from storage (missing) and an operator delete (manual) apart.
// Failed recordings may still own finalized media.
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

// A tombstone keeps its quality and size as an audit record, dimmed so the row
// reads as inactive rather than advertising bytes that are no longer there.
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

// whenColumn shows the most relevant moment for the row: when it was removed,
// else when it finished, else when it started. Relative ("2h ago") with an
// absolute hover via the shared Timestamp.
const whenColumn = (
	t: TFunction,
	locale: string,
): ColumnDef<VideoResponse> => ({
	id: "when",
	// The value exists only so TanStack lets the header sort: getCanSort()
	// requires an accessor, and manualSorting means the server does the
	// ordering. Mirrors the iso the cell renders.
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

// ErrorCell shows a failure message clamped to one line, click to expand the
// full text. Errors run long (ffmpeg/HLS dumps), so an inline expand reads far
// better than hover-truncation.
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

// historyColumns returns the column set for one view. Every column has to earn
// its place: Media says nothing once the scope pins it, Size is empty for a run
// that never wrote a file, and the error only exists on failures.
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
	// Every view can hold a restorable tombstone, so the actions column stays;
	// its cells decide per row.
	cols.push(actionsColumn(canManage));
	// Enable sorting only on columns the server can sort (the header drives a
	// real server-side sort + page reset; see HISTORY_SORT_BY_COLUMN). Status,
	// quality, error, and actions stay non-sortable.
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

// HISTORY_SORT_BY_COLUMN maps a sortable column's id to the server VideoSort key.
// Column ids are the accessorKey ("display_name", "size_bytes") or explicit id
// ("when"). Keep in lockstep with the column factories above.
export const HISTORY_SORT_BY_COLUMN: Record<
	string,
	"channel" | "size" | "history_when"
> = {
	display_name: "channel",
	size_bytes: "size",
	when: "history_when",
};
