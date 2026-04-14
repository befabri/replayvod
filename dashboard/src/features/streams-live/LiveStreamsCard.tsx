import { useTranslation } from "react-i18next";
import { useLiveStreams } from "./queries";

// LiveStreamsCard renders the last few stream.live events. Shows
// nothing when the feed is empty (typical steady state), so the
// dashboard only grows a card when there's fresh activity. A
// matched_schedules counter tells operators whether the auto-
// download pipeline actually kicked in.
export function LiveStreamsCard() {
	const { t } = useTranslation();
	const events = useLiveStreams(5);

	if (events.length === 0) return null;

	return (
		<div className="rounded-lg bg-card text-card-foreground p-4 shadow-sm sm:p-5">
			<h2 className="text-sm font-medium mb-3">{t("streams_live.title")}</h2>
			<ul className="space-y-2">
				{events.map((e, i) => (
					<li
						key={`${e.broadcaster_id}-${e.started_at}-${i}`}
						className="flex items-center justify-between gap-4 text-sm"
					>
						<div className="min-w-0 flex-1">
							<div className="truncate font-medium">{e.display_name}</div>
							<div className="text-xs text-muted-foreground font-mono">
								{e.broadcaster_login}
							</div>
						</div>
						<div className="text-right text-xs text-muted-foreground whitespace-nowrap">
							<div>
								{t("streams_live.matched", {
									count: e.matched_schedules,
								})}
							</div>
							{e.job_id && (
								<div className="font-mono truncate max-w-[12rem]">
									{e.job_id}
								</div>
							)}
						</div>
					</li>
				))}
			</ul>
		</div>
	);
}
