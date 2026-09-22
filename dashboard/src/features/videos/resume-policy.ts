import type {
	SettingsResponse,
	VideoResponse,
	VideoUserStateResponse,
} from "@/api/generated/trpc";

export type ResumePolicy = SettingsResponse["playback"];

export function resumeOffsetSeconds(
	state:
		| Pick<VideoUserStateResponse, "last_position_seconds">
		| null
		| undefined,
	totalDurationSeconds: number,
	policy: ResumePolicy,
): number | undefined {
	const position = state?.last_position_seconds;
	if (position == null || !Number.isFinite(position)) return undefined;
	if (position < policy.resume_min_seconds) return undefined;
	if (Number.isFinite(totalDurationSeconds) && totalDurationSeconds > 0) {
		const margin = Math.min(
			policy.resume_end_margin_seconds,
			(totalDurationSeconds * policy.resume_end_margin_percent) / 100,
		);
		if (position >= totalDurationSeconds - margin) return undefined;
	}
	return position;
}

export function isContinueWatchingVideo(
	video: Pick<
		VideoResponse,
		"status" | "deleted_at" | "user_state" | "duration_seconds"
	>,
	policy: ResumePolicy,
): boolean {
	return (
		video.status === "DONE" &&
		!video.deleted_at &&
		!!video.user_state?.watched_at &&
		resumeOffsetSeconds(
			video.user_state,
			video.duration_seconds ?? 0,
			policy,
		) != null
	);
}
