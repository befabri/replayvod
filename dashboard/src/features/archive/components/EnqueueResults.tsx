import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import type {
	ArchiveEnqueueStatus,
	EnqueueArchiveItem,
} from "@/api/generated/trpc";
import { Badge } from "@/components/ui/badge";

const STATUS_VARIANT: Record<
	ArchiveEnqueueStatus,
	"green" | "muted" | "yellow" | "red"
> = {
	queued: "green",
	exists: "muted",
	not_found: "yellow",
	invalid: "yellow",
	live: "yellow",
	private: "muted",
	error: "red",
};

// EnqueueResults lists what happened to each submitted link. Each line keeps
// the user's own input so a bad paste is easy to spot and fix.
export function EnqueueResults({ items }: { items: EnqueueArchiveItem[] }) {
	const { t } = useTranslation();
	if (items.length === 0) return null;
	// The same input can appear twice (a duplicate line), so key on the
	// input plus its occurrence rather than the array position.
	const seen = new Map<string, number>();
	const keyed = items.map((item) => {
		const n = (seen.get(item.input) ?? 0) + 1;
		seen.set(item.input, n);
		return { key: `${item.input}#${n}`, item };
	});
	return (
		<ul className="divide-y divide-border rounded-md border border-border text-sm">
			{keyed.map(({ key, item }) => (
				<li
					key={key}
					className="flex flex-wrap items-center justify-between gap-2 px-3 py-2"
				>
					<div className="min-w-0 flex-1">
						<div className="truncate font-medium">
							{item.title || item.vod_id || item.input}
						</div>
						<div className="truncate text-xs text-muted-foreground">
							{item.title ? item.input : item.message}
							{item.title && item.message ? ` · ${item.message}` : null}
						</div>
					</div>
					<div className="flex items-center gap-2">
						{item.video_id != null && item.status === "exists" ? (
							<Link
								to="/dashboard/watch/$videoId"
								params={{ videoId: String(item.video_id) }}
								search={{ t: undefined }}
								className="text-xs text-link hover:underline"
							>
								{t("archive.open_recording")}
							</Link>
						) : null}
						<Badge variant={STATUS_VARIANT[item.status]}>
							{t(`archive.result_${item.status}`)}
						</Badge>
					</div>
				</li>
			))}
		</ul>
	);
}
