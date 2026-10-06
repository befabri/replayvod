import type { TFunction } from "i18next";
import type { VideoResponse, VideoStatus } from "@/api/generated/trpc";
import { isRecordingQuality } from "@/lib/recording-settings";
import { formatDuration } from "./format";
import { recordingPartQualities } from "./metadata";

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

export function qualityLabel(t: TFunction, quality: string): string {
	if (quality === "audio_only") return t("videos.mode_audio");
	return isRecordingQuality(quality)
		? t(`videos.quality_${quality.toLowerCase()}`)
		: quality;
}

export function videoQualityLabel(
	t: TFunction,
	video: Pick<VideoResponse, "quality" | "is_audio_only">,
): string {
	return video.is_audio_only
		? t("videos.mode_audio")
		: qualityLabel(t, video.quality);
}

export function recordingQualityLabel(
	t: TFunction,
	video: Pick<VideoResponse, "quality" | "is_audio_only" | "parts">,
): string {
	if (video.is_audio_only) return t("videos.mode_audio");
	const labels = [
		...new Set(recordingPartQualities(video).map((q) => qualityLabel(t, q))),
	];
	if (labels.length === 0) return qualityLabel(t, video.quality);
	if (labels.length === 1) return labels[0];
	return `${t("videos.mixed_quality")} (${labels.join(", ")})`;
}

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

export function spanDurationLabel(seconds: number, t: TFunction): string {
	if (seconds <= 0) return t("videos.span_duration.just_switched");
	if (seconds < 60) return t("videos.span_duration.less_than_minute");
	return formatDuration(seconds);
}
