import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Avatar } from "@/components/ui/avatar";
import { useFollowedStreams } from "@/features/streams-live";
import { useTick } from "@/hooks/useTick";
import { formatRelative } from "@/lib/format-relative";

// LastLiveStatistics — shows currently-live followed channels with the
// Helix stream snapshot (title, game, viewer count, profile image).
// Server joined `profile_image_url` into the response, so no secondary
// channel catalog fetch is needed.
export function LastLiveStatistics() {
	const { t, i18n } = useTranslation();
	const { data, isLoading, isError } = useFollowedStreams();
	// Re-render once per minute so "5m ago" labels stay current.
	useTick(60_000);

	const items = (data ?? []).slice(0, 4);

	return (
		<div className="rounded-lg bg-card text-card-foreground p-4 shadow-sm sm:p-5">
			<h5 className="mb-4 text-xl font-medium text-foreground">
				{t("streams_live.title")}
			</h5>
			{isLoading ? (
				<div className="text-muted-foreground text-sm">
					{t("common.loading")}
				</div>
			) : isError ? (
				<div className="text-destructive text-sm">
					{t("videos.failed_to_load")}
				</div>
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
								size="lg"
								isLive
							/>
							<div className="min-w-0 flex-1">
								<Link
									// biome-ignore lint/suspicious/noExplicitAny: dynamic param route
									to={"/dashboard/channels/$channelId" as any}
									// biome-ignore lint/suspicious/noExplicitAny: dynamic param route
									params={{ channelId: s.broadcaster_id } as any}
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
