import type { TimelineEvent, VideoResponse } from "@/api/generated/trpc";
import {
	type TimelineChangedField,
	type TimelineMarkerKind,
	timelineChangeEvents,
	timelineEventKey,
	timelineEventOffsetSeconds,
	timelineMarkerKind,
	timelineMarkerLabel,
} from "@/features/videos/timeline";
import { clamp, positiveOrNull, positiveOrZero } from "@/lib/utils";

type VideoPart = NonNullable<VideoResponse["parts"]>[number];

export type RecordingPlaylistPart = {
	partIndex: number;
	position: number;
	src: string;
	mimeType: "video/mp4" | "audio/mp4";
	durationSeconds: number;
	sizeBytes: number | null;
	startSeconds: number;
	endSeconds: number;
	label: string;
};

export type RecordingPlaylistSource = {
	src: string;
	mimeType: "video/mp4" | "audio/mp4";
	// Probed duration of the continuous file, when known. The muxed artifact runs
	// on its own clock, which can drift from the recording timeline (summed part
	// EXTINF). The player reconciles that drift against `totalDurationSeconds` at
	// the I/O boundary; everything the UI renders stays on the recording basis.
	// Null when unknown — treat as no drift.
	durationSeconds: number | null;
};

export type RecordingMarkerKind = TimelineMarkerKind;

export type RecordingTimelineMarker = {
	key: string;
	offsetSeconds: number;
	label: string;
	kind: RecordingMarkerKind;
	changes: TimelineChangedField[];
	event: TimelineEvent;
};

export type RecordingPlaylist = {
	videoId: number;
	title: string;
	isAudioOnly: boolean;
	totalDurationSeconds: number;
	continuousSource: RecordingPlaylistSource | null;
	parts: RecordingPlaylistPart[];
	markers: RecordingTimelineMarker[];
};

export type PartSeekTarget = {
	part: RecordingPlaylistPart;
	localSeconds: number;
	globalSeconds: number;
};

export type ChapterCue = {
	startTime: number;
	endTime: number;
	text: string;
};

export function buildRecordingPlaylist(
	video: VideoResponse,
	timeline: TimelineEvent[] | undefined,
	apiUrl = "",
): RecordingPlaylist {
	const parts = buildPlaylistParts(video, apiUrl);
	// One timeline basis for rendering AND seeking: the recording timeline from
	// summed part durations (metadata markers are in real recording seconds, so
	// they live on this basis too). A ready muxed artifact may probe to a slightly
	// different total; that drift is reconciled at the player boundary (see
	// WatchPlayer), not by handing the UI a second timebase.
	const totalDurationSeconds = playlistDuration(parts, video.duration_seconds);
	return {
		videoId: video.id,
		title: video.title?.trim() || video.display_name,
		isAudioOnly: video.is_audio_only,
		totalDurationSeconds,
		continuousSource: continuousSourceForVideo(video, parts, apiUrl),
		parts,
		markers: buildRecordingMarkers(
			timeline,
			video.start_download_at,
			totalDurationSeconds,
		),
	};
}

export function continuousSourceForVideo(
	video: Pick<
		VideoResponse,
		"id" | "parts" | "playback_artifact" | "is_audio_only"
	>,
	parts: RecordingPlaylistPart[],
	apiUrl = "",
): RecordingPlaylistSource | null {
	if (parts.length === 0) return null;
	if (video.playback_artifact?.status === "ready") {
		return {
			src: mediaURL(apiUrl, `/api/v1/videos/${video.id}/playback/stream`),
			mimeType: video.is_audio_only
				? "audio/mp4"
				: mimeTypeForArtifact(video.playback_artifact),
			durationSeconds: artifactDurationSeconds(video.playback_artifact),
		};
	}
	if (parts.length === 1) {
		return {
			src: parts[0].src,
			mimeType: parts[0].mimeType,
			durationSeconds: positiveOrNull(parts[0].durationSeconds),
		};
	}
	return null;
}

// artifactDurationSeconds returns the ready artifact's probed duration, the
// length of the muxed file's own clock. The player uses it to reconcile that
// clock against the recording timeline. Null when there's no usable ready
// artifact (the player then assumes no drift).
function artifactDurationSeconds(
	artifact: VideoResponse["playback_artifact"],
): number | null {
	return artifact?.status === "ready"
		? positiveOrNull(artifact.duration_seconds)
		: null;
}

function mimeTypeForArtifact(
	artifact: NonNullable<VideoResponse["playback_artifact"]>,
): "video/mp4" | "audio/mp4" {
	return artifact.mime_type === "audio/mp4" ? "audio/mp4" : "video/mp4";
}

export function buildPlaylistParts(
	video: Pick<
		VideoResponse,
		"id" | "duration_seconds" | "parts" | "is_audio_only"
	>,
	apiUrl = "",
): RecordingPlaylistPart[] {
	const rows = [...(video.parts ?? [])].sort(
		(a, b) => a.part_index - b.part_index,
	);
	if (rows.length === 0) {
		const durationSeconds = positiveOrZero(video.duration_seconds);
		return [
			{
				partIndex: 0,
				position: 0,
				src: mediaURL(apiUrl, `/api/v1/videos/${video.id}/stream`),
				mimeType: video.is_audio_only ? "audio/mp4" : "video/mp4",
				durationSeconds,
				sizeBytes: null,
				startSeconds: 0,
				endSeconds: durationSeconds,
				label: "Part 1",
			},
		];
	}

	let cursor = 0;
	return rows.map((part, position) => {
		const durationSeconds = cleanPartDuration(
			part,
			rows,
			video.duration_seconds,
		);
		const startSeconds = cursor;
		const endSeconds = startSeconds + durationSeconds;
		cursor = endSeconds;
		return {
			partIndex: part.part_index,
			position,
			src: mediaURL(
				apiUrl,
				`/api/v1/videos/${video.id}/parts/${part.part_index}/stream`,
			),
			mimeType: video.is_audio_only ? "audio/mp4" : "video/mp4",
			durationSeconds,
			sizeBytes: positiveOrNull(part.size_bytes),
			startSeconds,
			endSeconds,
			label: `Part ${position + 1}`,
		};
	});
}

export function playlistDuration(
	parts: RecordingPlaylistPart[],
	fallbackSeconds: number | undefined,
): number {
	const summed = parts.reduce((total, part) => total + part.durationSeconds, 0);
	if (summed > 0) return summed;
	return positiveOrZero(fallbackSeconds);
}

export function findPartForOffset(
	parts: RecordingPlaylistPart[],
	offsetSeconds: number,
	totalDurationSeconds?: number,
): PartSeekTarget | null {
	if (parts.length === 0) return null;
	const requestedTotalSeconds = positiveOrZero(totalDurationSeconds);
	const totalSeconds =
		requestedTotalSeconds > 0
			? requestedTotalSeconds
			: playlistDuration(parts, undefined);
	const globalSeconds = clamp(
		positiveOrZero(offsetSeconds),
		0,
		Math.max(0, totalSeconds),
	);
	for (const part of parts) {
		if (globalSeconds < part.endSeconds || part.position === parts.length - 1) {
			return {
				part,
				localSeconds: clamp(
					globalSeconds - part.startSeconds,
					0,
					Math.max(0, part.durationSeconds),
				),
				globalSeconds,
			};
		}
	}
	return null;
}

export function globalTimeForPart(
	part: RecordingPlaylistPart,
	localSeconds: number,
): number {
	return part.startSeconds + clamp(localSeconds, 0, part.durationSeconds);
}

export function buildRecordingMarkers(
	timeline: TimelineEvent[] | undefined,
	wallClockAnchor: string,
	totalDurationSeconds: number,
): RecordingTimelineMarker[] {
	const maxOffset = positiveOrZero(totalDurationSeconds);
	return timelineChangeEvents(timeline)
		.map(({ event, changes }, index) => {
			const offsetSeconds = clamp(
				timelineEventOffsetSeconds(event, wallClockAnchor),
				0,
				maxOffset,
			);
			return {
				key: `${timelineEventKey(event)}:${index}`,
				offsetSeconds,
				label: timelineMarkerLabel(event, changes),
				kind: timelineMarkerKind(changes),
				changes,
				event,
				index,
			};
		})
		.filter((marker) => marker.label.length > 0)
		.sort((a, b) => a.offsetSeconds - b.offsetSeconds || a.index - b.index)
		.map(({ index: _index, ...marker }) => marker);
}

export function chapterCuesForPart(
	markers: RecordingTimelineMarker[],
	part: RecordingPlaylistPart,
): ChapterCue[] {
	if (part.durationSeconds <= 0) return [];
	const sorted = [...markers].sort((a, b) => a.offsetSeconds - b.offsetSeconds);
	const cues: Array<{ startTime: number; text: string }> = [];
	const activeAtStart = lastMarkerAtOrBefore(sorted, part.startSeconds);
	cues.push({
		startTime: 0,
		text: activeAtStart?.label || part.label,
	});
	for (const marker of sorted) {
		if (
			marker.offsetSeconds < part.startSeconds ||
			marker.offsetSeconds >= part.endSeconds
		) {
			continue;
		}
		const local = marker.offsetSeconds - part.startSeconds;
		const last = cues[cues.length - 1];
		if (Math.abs(local - last.startTime) < 0.001) {
			last.text = marker.label;
			continue;
		}
		cues.push({ startTime: local, text: marker.label });
	}
	return cues
		.map((cue, index) => ({
			startTime: cue.startTime,
			endTime: cues[index + 1]?.startTime ?? part.durationSeconds,
			text: cue.text,
		}))
		.filter((cue) => cue.endTime - cue.startTime > 0.001);
}

export function chapterCuesForRecording(
	markers: RecordingTimelineMarker[],
	totalDurationSeconds: number,
	fallbackLabel: string,
): ChapterCue[] {
	const duration = positiveOrZero(totalDurationSeconds);
	if (duration <= 0) return [];
	const sorted = [...markers].sort((a, b) => a.offsetSeconds - b.offsetSeconds);
	const cues: Array<{ startTime: number; text: string }> = [
		{
			startTime: 0,
			text: sorted[0]?.offsetSeconds === 0 ? sorted[0].label : fallbackLabel,
		},
	];
	for (const marker of sorted) {
		if (marker.offsetSeconds < 0 || marker.offsetSeconds >= duration) continue;
		const last = cues[cues.length - 1];
		if (Math.abs(marker.offsetSeconds - last.startTime) < 0.001) {
			last.text = marker.label;
			continue;
		}
		cues.push({ startTime: marker.offsetSeconds, text: marker.label });
	}
	return cues
		.map((cue, index) => ({
			startTime: cue.startTime,
			endTime: cues[index + 1]?.startTime ?? duration,
			text: cue.text,
		}))
		.filter((cue) => cue.endTime - cue.startTime > 0.001);
}

function cleanPartDuration(
	part: VideoPart,
	allParts: VideoPart[],
	videoDurationSeconds: number | undefined,
): number {
	const exact = positiveOrZero(part.duration_seconds);
	if (exact > 0) return exact;
	if (allParts.length === 1) return positiveOrZero(videoDurationSeconds);
	return 0;
}

function mediaURL(apiUrl: string, path: string): string {
	return `${apiUrl}${path}`;
}

function lastMarkerAtOrBefore(
	markers: RecordingTimelineMarker[],
	offsetSeconds: number,
): RecordingTimelineMarker | null {
	let current: RecordingTimelineMarker | null = null;
	for (const marker of markers) {
		if (marker.offsetSeconds > offsetSeconds) break;
		current = marker;
	}
	return current;
}
