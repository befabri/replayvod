import type { TFunction } from "i18next";
import type { VideoResponse, VideoStatus } from "@/api/generated/trpc";
import { formatDuration } from "./format";

// LabeledStatus is the generated wire VideoStatus union plus the UI-only
// CANCELLED, which the server never sends as a status. It's rendered for a
// FAILED recording whose completion_kind is "cancelled".
type LabeledStatus = VideoStatus | "CANCELLED";
type VideoStatusLabelKey = `videos.status.${LabeledStatus}`;

const VIDEO_STATUS_LABEL_KEYS: Record<LabeledStatus, VideoStatusLabelKey> = {
	PENDING: "videos.status.PENDING",
	RUNNING: "videos.status.RUNNING",
	DONE: "videos.status.DONE",
	FAILED: "videos.status.FAILED",
	CANCELLED: "videos.status.CANCELLED",
};

function isLabeledStatus(status: string): status is LabeledStatus {
	return Object.hasOwn(VIDEO_STATUS_LABEL_KEYS, status);
}

export function videoStatusLabel(t: TFunction, status: string): string {
	return isLabeledStatus(status)
		? t(VIDEO_STATUS_LABEL_KEYS[status], status)
		: status;
}

// channelLabel returns the best human-readable channel name for a
// video row: broadcaster_name if populated, otherwise login,
// otherwise the raw id. Kept as a shared helper so card, table and
// sort accessor stay aligned — previously each site recomputed the
// fallback chain and could drift independently.
export function channelLabel(video: {
	broadcaster_name?: VideoResponse["broadcaster_name"];
	broadcaster_login?: VideoResponse["broadcaster_login"];
	broadcaster_id: VideoResponse["broadcaster_id"];
}): string {
	return (
		video.broadcaster_name?.trim() ||
		video.broadcaster_login?.trim() ||
		video.broadcaster_id
	);
}

// spanDurationLabel renders the duration line for title and category
// history entries. Handles the two degenerate cases — the span is
// open (server reports 0, meaning the stream just switched into it)
// and the span is tiny (<1 min, so mm:ss would read "0:30" and feel
// like a data bug) — without pushing that logic into every consumer.
export function spanDurationLabel(seconds: number, t: TFunction): string {
	if (seconds <= 0) return t("videos.span_duration.just_switched");
	if (seconds < 60) return t("videos.span_duration.less_than_minute");
	return formatDuration(seconds);
}
