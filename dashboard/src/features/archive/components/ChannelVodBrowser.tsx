import { ArrowSquareOutIcon } from "@phosphor-icons/react";
import type { ColumnDef, RowSelectionState } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { useId, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import type {
	EnqueueArchiveItem,
	TwitchVODResponse,
} from "@/api/generated/trpc";
import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { DataTable } from "@/components/ui/data-table";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { chunk, MAX_VODS_PER_ENQUEUE } from "@/features/archive/limits";
import { parseChannelInput } from "@/features/archive/parse";
import { useChannelVods, useEnqueueArchive } from "@/features/archive/queries";
import {
	type ArchiveSettings,
	archivePayloadSettings,
} from "@/features/archive/settings";
import { VideoStatusBadge } from "@/features/videos/components/VideoStatusBadge";
import { formatDuration } from "@/features/videos/format";
import { formatAbsolute } from "@/lib/format-relative";
import { cn } from "@/lib/utils";

export function ChannelVodBrowser({ settings }: { settings: ArchiveSettings }) {
	const { t, i18n } = useTranslation();
	const id = useId();
	const [query, setQuery] = useState("");
	const [channel, setChannel] = useState<string | null>(null);
	const [selected, setSelected] = useState<RowSelectionState>({});
	const vods = useChannelVods(channel);
	const enqueue = useEnqueueArchive();

	const login = parseChannelInput(query);
	const rows = useMemo(
		() => vods.data?.pages.flatMap((page) => page.vods) ?? [],
		[vods.data],
	);
	const header = vods.data?.pages[0]?.channel;
	const selectedCount = Object.keys(selected).length;

	const columns = useMemo(
		() => browserColumns(t, i18n.language),
		[t, i18n.language],
	);

	const lookup = () => {
		if (!login) return;
		setSelected({});
		setChannel(login);
	};

	const archiveSelected = async () => {
		const ids = rows
			.filter((row) => selected[row.id] && canArchive(row))
			.map((row) => row.id);
		if (ids.length === 0) return;
		try {
			const items: EnqueueArchiveItem[] = [];
			for (const batch of chunk(ids, MAX_VODS_PER_ENQUEUE)) {
				const response = await enqueue.mutateAsync({
					vods: batch,
					...archivePayloadSettings(settings),
				});
				items.push(...response.items);
			}
			const queued = items.filter((i) => i.status === "queued").length;
			const failed = items.filter((i) => i.status === "error").length;
			if (queued > 0) {
				toast.success(t("archive.queued_toast", { count: queued }));
			} else {
				toast.info(t("archive.nothing_queued_toast"));
			}
			if (failed > 0) {
				toast.error(t("archive.some_failed_toast", { count: failed }));
			}
			setSelected({});
		} catch (err) {
			toast.error(
				err instanceof Error && err.message
					? err.message
					: t("archive.enqueue_failed"),
			);
		}
	};

	return (
		<div className="space-y-4">
			<form
				onSubmit={(e) => {
					e.preventDefault();
					lookup();
				}}
				className="space-y-2"
			>
				<Label htmlFor={`${id}-channel`}>{t("archive.channel_label")}</Label>
				<div className="flex gap-2">
					<Input
						id={`${id}-channel`}
						value={query}
						onChange={(e) => setQuery(e.target.value)}
						placeholder={t("archive.channel_placeholder")}
						autoComplete="off"
						aria-invalid={query.trim() !== "" && login == null}
					/>
					<Button type="submit" disabled={login == null || vods.isFetching}>
						{vods.isFetching && vods.data == null
							? t("common.loading")
							: t("archive.channel_lookup")}
					</Button>
				</div>
			</form>

			{vods.isError ? (
				<Alert variant="destructive">
					{vods.error.message || t("archive.channel_failed")}
				</Alert>
			) : null}

			{header ? (
				<div className="flex items-center gap-3">
					<Avatar src={header.profile_image_url} name={header.name} size="md" />
					<div className="min-w-0">
						<div className="truncate font-medium">{header.name}</div>
						<div className="truncate text-xs text-muted-foreground">
							{t("archive.channel_vod_count", { count: rows.length })}
						</div>
					</div>
				</div>
			) : null}

			{vods.data ? (
				<>
					<DataTable
						columns={columns}
						data={rows}
						emptyMessage={t("archive.channel_empty")}
						getRowId={(row) => row.id}
						rowSelection={selected}
						onRowSelectionChange={setSelected}
						enableRowSelection={(row) => canArchive(row.original)}
					/>
					<div className="flex flex-wrap items-center justify-between gap-3">
						<div>
							{vods.hasNextPage ? (
								<Button
									type="button"
									variant="outline"
									size="sm"
									onClick={() => void vods.fetchNextPage()}
									disabled={vods.isFetchingNextPage}
								>
									{vods.isFetchingNextPage
										? t("common.loading")
										: t("archive.load_more")}
								</Button>
							) : null}
						</div>
						<Button
							type="button"
							onClick={() => void archiveSelected()}
							disabled={selectedCount === 0 || enqueue.isPending}
						>
							{enqueue.isPending
								? t("common.saving")
								: selectedCount > 0
									? t("archive.archive_selected_count", {
											count: selectedCount,
										})
									: t("archive.archive_selected")}
						</Button>
					</div>
				</>
			) : null}
		</div>
	);
}

function browserColumns(
	t: TFunction,
	locale: string,
): ColumnDef<TwitchVODResponse>[] {
	return [
		{
			id: "select",
			header: ({ table }) => (
				<Checkbox
					aria-label={t("archive.select_all")}
					checked={table.getIsAllRowsSelected()}
					disabled={!table.getRowModel().rows.some((row) => row.getCanSelect())}
					onCheckedChange={(checked) =>
						table.toggleAllRowsSelected(checked === true)
					}
				/>
			),
			cell: ({ row }) =>
				row.getCanSelect() ? (
					<Checkbox
						aria-label={t("archive.select_vod", { title: row.original.title })}
						checked={row.getIsSelected()}
						onCheckedChange={(checked) => row.toggleSelected(checked === true)}
					/>
				) : null,
		},
		{
			accessorKey: "title",
			header: t("archive.col_vod"),
			cell: ({ row }) => <VodCell vod={row.original} t={t} />,
		},
		{
			accessorKey: "created_at",
			header: t("archive.col_streamed"),
			cell: ({ row }) => (
				<span className="whitespace-nowrap text-sm tabular-nums">
					{formatAbsolute(row.original.created_at, locale)}
				</span>
			),
		},
		{
			accessorKey: "duration_seconds",
			header: t("archive.col_duration"),
			cell: ({ row }) => (
				<span className="font-mono text-sm tabular-nums">
					{formatDuration(row.original.duration_seconds)}
				</span>
			),
		},
		{
			accessorKey: "type",
			header: t("archive.col_type"),
			cell: ({ row }) => (
				<span className="text-sm text-muted-foreground">
					{vodTypeLabel(t, row.original.type)}
				</span>
			),
		},
		{
			id: "status",
			header: t("archive.col_status"),
			cell: ({ row }) => <StatusCell vod={row.original} t={t} />,
		},
	];
}

function VodCell({ vod, t }: { vod: TwitchVODResponse; t: TFunction }) {
	return (
		<div className="flex min-w-0 items-center gap-3">
			{vod.thumbnail_url ? (
				<img
					src={vod.thumbnail_url}
					alt=""
					width={96}
					height={54}
					loading="lazy"
					className="h-[54px] w-24 shrink-0 rounded-md bg-muted object-cover"
				/>
			) : (
				<div className="h-[54px] w-24 shrink-0 rounded-md bg-muted" />
			)}
			<div className="min-w-0">
				<div
					className={cn(
						"max-w-md truncate font-medium",
						vod.viewable === "private" && "text-muted-foreground",
					)}
					title={vod.title}
				>
					{vod.title || vod.id}
				</div>
				<a
					href={vod.url}
					target="_blank"
					rel="noreferrer"
					className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-link"
				>
					{t("archive.open_on_twitch")}
					<ArrowSquareOutIcon aria-hidden="true" className="size-3" />
				</a>
			</div>
		</div>
	);
}

function vodTypeLabel(t: TFunction, type: string): string {
	switch (type) {
		case "archive":
			return t("archive.type_archive");
		case "highlight":
			return t("archive.type_highlight");
		case "upload":
			return t("archive.type_upload");
		default:
			return type;
	}
}

function canArchive(vod: TwitchVODResponse): boolean {
	return (
		vod.archived_video_id == null && !vod.live && vod.viewable !== "private"
	);
}

function StatusCell({ vod, t }: { vod: TwitchVODResponse; t: TFunction }) {
	if (vod.archived_status) {
		return (
			<span className="inline-flex flex-wrap items-center gap-1.5">
				{vod.held_reason === "live_recording" ? (
					<Badge variant="muted">{t("archive.recorded_live")}</Badge>
				) : null}
				<VideoStatusBadge status={vod.archived_status} t={t} />
			</span>
		);
	}
	if (vod.live) {
		return <Badge variant="red">{t("archive.live_now")}</Badge>;
	}
	if (vod.viewable === "private") {
		return <Badge variant="muted">{t("archive.private")}</Badge>;
	}
	return <Badge variant="outline">{t("archive.on_twitch")}</Badge>;
}
