import { FunnelSimple, Rows, SortAscending } from "@phosphor-icons/react";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Button } from "@/components/ui/button";
import { DataTable } from "@/components/ui/data-table";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useVideos, type VideoOrder, type VideoSort } from "@/features/videos";
import { videoListColumns } from "@/features/videos/components/listColumns";
import { VideoCard } from "@/features/videos/components/VideoCard";

const PAGE_SIZE = 50;

type ViewMode = "grid" | "table";
type SortKey =
	| "newest"
	| "oldest"
	| "channel_asc"
	| "channel_desc"
	| "longest"
	| "largest";

const SORT_CONFIG: Record<SortKey, { sort: VideoSort; order: VideoOrder }> = {
	newest: { sort: "created_at", order: "desc" },
	oldest: { sort: "created_at", order: "asc" },
	channel_asc: { sort: "channel", order: "asc" },
	channel_desc: { sort: "channel", order: "desc" },
	longest: { sort: "duration", order: "desc" },
	largest: { sort: "size", order: "desc" },
};

const SORT_KEYS = Object.keys(SORT_CONFIG) as SortKey[];
const VIEW_MODES: ViewMode[] = ["grid", "table"];
const STATUS_KEYS = ["all", "DONE", "RUNNING", "PENDING", "FAILED"] as const;
type StatusKey = (typeof STATUS_KEYS)[number];

// Valid status values the URL may carry. "all" is the explicit
// opt-out; any other value (undefined, garbage) falls back to
// "DONE" so the landing view shows only watchable videos.
// Failed/in-flight rows live in /dashboard/activity/{queue,history}
// by design — the Videos page is the library, not a download log.
const VALID_STATUS: readonly StatusKey[] = STATUS_KEYS;

export const Route = createFileRoute("/dashboard/videos")({
	validateSearch: (search: Record<string, unknown>) => ({
		status:
			typeof search.status === "string" &&
			VALID_STATUS.includes(search.status as StatusKey)
				? (search.status as StatusKey)
				: ("DONE" as StatusKey),
		view:
			search.view === "table" || search.view === "grid"
				? (search.view as ViewMode)
				: ("grid" as ViewMode),
		sort: SORT_KEYS.includes(search.sort as SortKey)
			? (search.sort as SortKey)
			: ("newest" as SortKey),
	}),
	component: VideosPage,
});

function VideosPage() {
	const { t } = useTranslation();
	const { status, view, sort: sortKey } = Route.useSearch();
	const navigate = Route.useNavigate();
	const [page, setPage] = useState(0);

	const sortConfig = SORT_CONFIG[sortKey];

	// "all" means "no status filter" on the wire — the server's
	// ListVideosOpts treats empty string as unfiltered. Any other
	// StatusKey is passed through verbatim.
	const { data, isLoading, error } = useVideos(
		PAGE_SIZE,
		page * PAGE_SIZE,
		status === "all" ? undefined : status,
		sortConfig.sort,
		sortConfig.order,
	);

	const columns = useMemo(() => videoListColumns(t), [t]);

	return (
		<TitledLayout
			title={t("videos.title")}
			actions={
				<>
					<SortDropdown
						current={sortKey}
						onChange={(next) => {
							setPage(0);
							void navigate({ search: (s) => ({ ...s, sort: next }) });
						}}
					/>
					<ViewDropdown
						current={view}
						onChange={(next) => {
							setPage(0);
							void navigate({ search: (s) => ({ ...s, view: next }) });
						}}
					/>
					<StatusDropdown
						current={status}
						onChange={(next) => {
							setPage(0);
							void navigate({ search: (s) => ({ ...s, status: next }) });
						}}
					/>
				</>
			}
		>
			{isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}

			{error && (
				<div className="rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
					{t("videos.failed_to_load")}: {error.message}
				</div>
			)}

			{data && data.length === 0 && !isLoading && !error && (
				<div className="text-muted-foreground">{t("videos.empty")}</div>
			)}

			{data && data.length > 0 && (
				<>
					{view === "grid" ? (
						<div className="grid grid-cols-[repeat(auto-fit,minmax(400px,1fr))] gap-4">
							{data.map((v) => (
								<VideoCard key={v.id} video={v} />
							))}
						</div>
					) : (
						<DataTable
							columns={columns}
							data={data}
							emptyMessage={t("videos.empty")}
						/>
					)}
					<div className="flex items-center gap-2 mt-6">
						<Button
							variant="outline"
							size="sm"
							disabled={page === 0}
							onClick={() => setPage((p) => Math.max(0, p - 1))}
						>
							{t("videos.previous")}
						</Button>
						<span className="text-sm text-muted-foreground">
							{t("videos.page", { n: page + 1 })}
						</span>
						<Button
							variant="outline"
							size="sm"
							disabled={data.length < PAGE_SIZE}
							onClick={() => setPage((p) => p + 1)}
						>
							{t("videos.next")}
						</Button>
					</div>
				</>
			)}
		</TitledLayout>
	);
}

function SortDropdown({
	current,
	onChange,
}: {
	current: SortKey;
	onChange: (key: SortKey) => void;
}) {
	const { t } = useTranslation();
	return (
		<DropdownMenu>
			<DropdownMenuTrigger
				render={(triggerProps) => (
					<Button variant="outline" size="sm" {...triggerProps}>
						<SortAscending className="size-4" />
						{t(`videos.sort.${current}`)}
					</Button>
				)}
			/>
			<DropdownMenuContent>
				{SORT_KEYS.map((key) => (
					<DropdownMenuItem key={key} onClick={() => onChange(key)}>
						{t(`videos.sort.${key}`)}
					</DropdownMenuItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}

function ViewDropdown({
	current,
	onChange,
}: {
	current: ViewMode;
	onChange: (mode: ViewMode) => void;
}) {
	const { t } = useTranslation();
	return (
		<DropdownMenu>
			<DropdownMenuTrigger
				render={(triggerProps) => (
					<Button variant="outline" size="sm" {...triggerProps}>
						<Rows className="size-4" />
						{t(`videos.view.${current}`)}
					</Button>
				)}
			/>
			<DropdownMenuContent>
				{VIEW_MODES.map((mode) => (
					<DropdownMenuItem key={mode} onClick={() => onChange(mode)}>
						{t(`videos.view.${mode}`)}
					</DropdownMenuItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}

function StatusDropdown({
	current,
	onChange,
}: {
	current: StatusKey;
	onChange: (key: StatusKey) => void;
}) {
	const { t } = useTranslation();
	const label = (key: StatusKey) =>
		t(`videos.status_filter.${key === "all" ? "all" : key.toLowerCase()}`);
	return (
		<DropdownMenu>
			<DropdownMenuTrigger
				render={(triggerProps) => (
					<Button variant="outline" size="sm" {...triggerProps}>
						<FunnelSimple className="size-4" />
						{label(current)}
					</Button>
				)}
			/>
			<DropdownMenuContent>
				{STATUS_KEYS.map((key) => (
					<DropdownMenuItem key={key} onClick={() => onChange(key)}>
						{label(key)}
					</DropdownMenuItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
