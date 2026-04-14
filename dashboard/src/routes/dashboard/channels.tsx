import { Broadcast, SortAscending } from "@phosphor-icons/react";
import { Link, createFileRoute } from "@tanstack/react-router";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useChannels } from "@/features/channels";
import { useLiveSet } from "@/features/streams-live";

const SORT_MODES = ["name_asc", "name_desc"] as const;
type SortMode = (typeof SORT_MODES)[number];

const FILTER_MODES = ["all", "live"] as const;
type FilterMode = (typeof FILTER_MODES)[number];

export const Route = createFileRoute("/dashboard/channels")({
	validateSearch: (search: Record<string, unknown>) => ({
		sort: SORT_MODES.includes(search.sort as SortMode)
			? (search.sort as SortMode)
			: ("name_asc" as SortMode),
		filter: FILTER_MODES.includes(search.filter as FilterMode)
			? (search.filter as FilterMode)
			: ("all" as FilterMode),
	}),
	component: ChannelsPage,
});

function ChannelsPage() {
	const { t } = useTranslation();
	const { sort, filter } = Route.useSearch();
	const navigate = Route.useNavigate();
	const { data: channels, isLoading, error } = useChannels();
	const liveSet = useLiveSet();

	const visible = useMemo(() => {
		if (!channels) return channels;
		const pool =
			filter === "live"
				? channels.filter((c) => liveSet.has(c.broadcaster_id))
				: channels;
		const copy = [...pool];
		copy.sort((a, b) => a.broadcaster_name.localeCompare(b.broadcaster_name));
		return sort === "name_desc" ? copy.reverse() : copy;
	}, [channels, liveSet, sort, filter]);

	return (
		<TitledLayout
			title={t("nav.channels")}
			actions={
				<>
					<FilterDropdown
						current={filter}
						onChange={(next) =>
							void navigate({ search: (s) => ({ ...s, filter: next }) })
						}
					/>
					<SortDropdown
						current={sort}
						onChange={(next) =>
							void navigate({ search: (s) => ({ ...s, sort: next }) })
						}
					/>
				</>
			}
		>
			{isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}

			{error && (
				<div className="rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
					{t("channels.failed_to_load")}: {error.message}
				</div>
			)}

			{visible && visible.length === 0 && !isLoading && !error && (
				<div className="text-muted-foreground">{t("channels.empty")}</div>
			)}

			{visible && visible.length > 0 && (
				<div className="grid grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-2">
					{visible.map((c) => (
						<Link
							key={c.broadcaster_id}
							// biome-ignore lint/suspicious/noExplicitAny: param route typing
							to={"/dashboard/channels/$channelId" as any}
							// biome-ignore lint/suspicious/noExplicitAny: param route typing
							params={{ channelId: c.broadcaster_id } as any}
							className="flex items-center gap-3 rounded-md bg-card px-3 py-2 shadow-sm hover:bg-accent hover:text-accent-foreground transition-colors duration-75"
						>
							<Avatar
								src={c.profile_image_url}
								name={c.broadcaster_name}
								alt={c.broadcaster_name}
								size="md"
								isLive={liveSet.has(c.broadcaster_id)}
							/>
							<span className="truncate text-sm font-medium">
								{c.broadcaster_name}
							</span>
						</Link>
					))}
				</div>
			)}
		</TitledLayout>
	);
}

function FilterDropdown({
	current,
	onChange,
}: {
	current: FilterMode;
	onChange: (m: FilterMode) => void;
}) {
	const { t } = useTranslation();
	const labels: Record<FilterMode, string> = {
		all: t("channels.filter_all"),
		live: t("channels.filter_live"),
	};
	return (
		<DropdownMenu>
			<DropdownMenuTrigger
				render={(triggerProps) => (
					<Button variant="outline" size="sm" {...triggerProps}>
						<Broadcast className="size-4" />
						{labels[current]}
					</Button>
				)}
			/>
			<DropdownMenuContent>
				{FILTER_MODES.map((mode) => (
					<DropdownMenuItem key={mode} onClick={() => onChange(mode)}>
						{labels[mode]}
					</DropdownMenuItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}

function SortDropdown({
	current,
	onChange,
}: {
	current: SortMode;
	onChange: (m: SortMode) => void;
}) {
	const { t } = useTranslation();
	const labels: Record<SortMode, string> = {
		name_asc: t("channels.sort_asc"),
		name_desc: t("channels.sort_desc"),
	};
	return (
		<DropdownMenu>
			<DropdownMenuTrigger
				render={(triggerProps) => (
					<Button variant="outline" size="sm" {...triggerProps}>
						<SortAscending className="size-4" />
						{labels[current]}
					</Button>
				)}
			/>
			<DropdownMenuContent>
				{SORT_MODES.map((mode) => (
					<DropdownMenuItem key={mode} onClick={() => onChange(mode)}>
						{labels[mode]}
					</DropdownMenuItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
