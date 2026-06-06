import { Link } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { useState } from "react";
import { TimestampValue } from "@/components/ui/timestamp";
import { channelLabel, type VideoResponse } from "@/features/videos";
import { formatBytes } from "@/features/videos/format";
import { cn } from "@/lib/utils";
import { RemovedBadge } from "./RemovedBadge";
import { RemoveVideoButton } from "./RemoveVideoButton";
import { VideoStatusBadge } from "./VideoStatusBadge";

// HistoryFilter scopes the audit log. "all" is every terminal-or-removed
// recording (the default), "failed" is failures, "removed" is the tombstoned
// set (the rows the videos library can't show).
export type HistoryFilter = "all" | "failed" | "removed";

function channelColumn(t: TFunction): ColumnDef<VideoResponse> {
	return {
		accessorKey: "display_name",
		header: t("history.col_channel"),
		cell: ({ row }) => <ChannelCell row={row.original} />,
	};
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

// statusColumn renders the recording's status plus a RemovedBadge for tombstoned
// rows, so a removed recording shows both what it was and that it's gone.
function statusColumn(t: TFunction): ColumnDef<VideoResponse> {
	return {
		accessorKey: "status",
		header: t("history.col_status"),
		cell: ({ row }) => (
			<span className="inline-flex flex-wrap items-center gap-1.5">
				<VideoStatusBadge
					status={row.original.status}
					completionKind={row.original.completion_kind}
					t={t}
				/>
				{row.original.deleted_at ? (
					<RemovedBadge deletionKind={row.original.deletion_kind} t={t} />
				) : null}
			</span>
		),
	};
}

const qualityColumn = (t: TFunction): ColumnDef<VideoResponse> => ({
	accessorKey: "quality",
	header: t("history.col_quality"),
});

const sizeColumn = (t: TFunction): ColumnDef<VideoResponse> => ({
	accessorKey: "size_bytes",
	header: t("history.col_size"),
	cell: ({ row }) => (
		<span className="text-xs text-muted-foreground">
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

function actionsColumn(
	t: TFunction,
	canManage: boolean,
): ColumnDef<VideoResponse> {
	return {
		id: "actions",
		header: "",
		cell: ({ row }) => {
			const v = row.original;
			const isDone = v.status === "DONE";
			// Tombstones are gone (no actions); in-flight rows are managed from
			// Downloads. A present DONE recording can be watched; present DONE and
			// FAILED recordings can be removed. FAILED has no Watch — the player
			// only serves DONE recordings — so a FAILED row offers a viewer nothing
			// and its cell collapses to null.
			if (v.deleted_at || (!isDone && v.status !== "FAILED")) {
				return null;
			}
			if (!isDone && !canManage) {
				return null;
			}
			return (
				<div className="flex items-center justify-end gap-2">
					{isDone ? (
						<Link
							to="/dashboard/watch/$videoId"
							params={{ videoId: String(v.id) }}
							search={{ t: undefined }}
							className="text-primary text-xs hover:underline"
						>
							{t("videos.watch")}
						</Link>
					) : null}
					{canManage ? <RemoveVideoButton videoId={v.id} /> : null}
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

// historyColumns returns the column set for one history filter. Columns are
// outcome-specific: failures surface the (expandable) error, removed rows drop
// the actions column since they're already gone.
export function historyColumns(
	t: TFunction,
	filter: HistoryFilter,
	canManage: boolean,
	locale: string,
): ColumnDef<VideoResponse>[] {
	let cols: ColumnDef<VideoResponse>[];
	if (filter === "removed") {
		cols = [
			channelColumn(t),
			statusColumn(t),
			qualityColumn(t),
			sizeColumn(t),
			whenColumn(t, locale),
		];
	} else if (filter === "failed") {
		cols = [
			channelColumn(t),
			statusColumn(t),
			qualityColumn(t),
			whenColumn(t, locale),
			errorColumn(t),
			actionsColumn(t, canManage),
		];
	} else {
		cols = [
			channelColumn(t),
			statusColumn(t),
			qualityColumn(t),
			sizeColumn(t),
			whenColumn(t, locale),
			actionsColumn(t, canManage),
		];
	}
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
