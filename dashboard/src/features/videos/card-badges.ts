import type { VideoResponse, VideoStatus } from "@/api/generated/trpc";

export type VideoCardBadgeTone = "neutral" | "blue" | "red" | "yellow";

type BadgeLabelKey =
	| "videos.card_status.queued"
	| "videos.card_status.recording"
	| "videos.card_status.incomplete"
	| "videos.status.FAILED"
	| "videos.status.CANCELLED";

type BadgeTooltipKey =
	| "videos.card_status.partial_tooltip"
	| "videos.card_status.truncated_tooltip"
	| "videos.card_status.partial_truncated_tooltip"
	| "videos.completion.cancelled_tooltip";

type VideoCardBadge = {
	labelKey: BadgeLabelKey;
	tone: VideoCardBadgeTone;
	tooltipKey?: BadgeTooltipKey;
};

const STATUS_BADGES = {
	PENDING: { labelKey: "videos.card_status.queued", tone: "neutral" },
	RUNNING: { labelKey: "videos.card_status.recording", tone: "blue" },
	DONE: null,
	FAILED: { labelKey: "videos.status.FAILED", tone: "red" },
	CANCELLED: {
		labelKey: "videos.status.CANCELLED",
		tone: "neutral",
		tooltipKey: "videos.completion.cancelled_tooltip",
	},
	INCOMPLETE: { labelKey: "videos.card_status.incomplete", tone: "yellow" },
} satisfies Record<
	VideoStatus | "CANCELLED" | "INCOMPLETE",
	VideoCardBadge | null
>;

const INCOMPLETE_TOOLTIPS = {
	partial: "videos.card_status.partial_tooltip",
	truncated: "videos.card_status.truncated_tooltip",
	partial_truncated: "videos.card_status.partial_truncated_tooltip",
} satisfies Record<string, BadgeTooltipKey>;

export function videoCardStatusBadge(
	video: Pick<VideoResponse, "status" | "completion_kind" | "truncated">,
): VideoCardBadge | null {
	if (video.status === "FAILED" && video.completion_kind === "cancelled") {
		return STATUS_BADGES.CANCELLED;
	}

	const partial = video.completion_kind === "partial";
	if (video.status === "DONE" && (partial || video.truncated)) {
		const tooltip =
			partial && video.truncated
				? "partial_truncated"
				: partial
					? "partial"
					: "truncated";
		return {
			...STATUS_BADGES.INCOMPLETE,
			tooltipKey: INCOMPLETE_TOOLTIPS[tooltip],
		};
	}

	return STATUS_BADGES[video.status];
}
