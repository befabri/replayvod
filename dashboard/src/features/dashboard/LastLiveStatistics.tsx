import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ViewAllLink } from "@/components/ui/view-all-link";
import { useFollowedStreams } from "@/features/streams-live";
import { useTick } from "@/hooks/useTick";
import { formatRelative } from "@/lib/format-relative";
import { AvatarRowsSkeleton } from "./AvatarRowsSkeleton";

export function LastLiveStatistics() {
	const { t, i18n } = useTranslation();
	const { data, isLoading, isError, isFetching, refetch } =
		useFollowedStreams();
	useTick(60_000);

	const items = (data ?? []).slice(0, 4);
	const total = (data ?? []).length;

	return (
		<div className="rounded-lg bg-card text-card-foreground p-4 shadow-sm sm:p-5">
			<div className="mb-4 flex items-center justify-between gap-3">
				<h5 className="text-xl font-medium text-foreground">
					{t("streams_live.title")}
				</h5>
				{total > 0 ? (
					<ViewAllLink
						to="/dashboard/channels"
						search={{ filter: "live", sort: "name_asc" }}
					/>
				) : null}
			</div>
			{isLoading ? (
				<AvatarRowsSkeleton
					subtitle
					rowClassName="gap-3"
					trailing={<Skeleton className="h-3 w-12 shrink-0" />}
				/>
			) : isError ? (
				<Alert
					variant="destructive"
					action={
						<Button
							variant="outline"
							size="sm"
							disabled={isFetching}
							onClick={() => void refetch()}
						>
							{t("common.retry")}
						</Button>
					}
				>
					{t("streams_live.failed_to_load")}
				</Alert>
			) : items.length === 0 ? (
				<div className="text-muted-foreground text-sm">
					{t("streams_live.empty")}
				</div>
			) : (
				<ul className="divide-y divide-border">
					{items.map((s) => (
						<li
							key={s.stream_id}
							className="flex items-center gap-3 py-2 first:pt-0 last:pb-0"
						>
							<Avatar
								src={s.profile_image_url}
								name={s.broadcaster_name}
								alt={s.broadcaster_name}
								size="md"
								isLive
							/>
							<div className="min-w-0 flex-1">
								<Link
									to="/dashboard/channels/$channelId"
									params={{ channelId: s.broadcaster_id }}
									className="truncate text-sm font-medium text-foreground hover:text-link"
								>
									{s.broadcaster_name}
								</Link>
								<div className="truncate text-xs text-muted-foreground">
									{s.game_name || s.title}
								</div>
							</div>
							<div className="text-right text-xs text-muted-foreground whitespace-nowrap">
								{formatRelative(s.started_at, i18n.language)}
							</div>
						</li>
					))}
				</ul>
			)}
		</div>
	);
}
