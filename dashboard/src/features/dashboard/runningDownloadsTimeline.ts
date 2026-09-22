import type {
	ActiveDownloadResponse,
	TimelineEvent,
} from "@/api/generated/trpc";
import { orderedVideoParts } from "@/features/videos/metadata";
import {
	type TimelineMarkerKind,
	timelineChangeEvents,
	timelineEventKey,
	timelineEventOffsetSeconds,
	timelineMarkerKind,
	timelineMarkerLabel,
} from "@/features/videos/timeline";
import { positiveOrZero } from "@/lib/utils";

export type MetadataMarker = {
	key: string;
	offsetSeconds: number;
	label: string;
	kind: TimelineMarkerKind;
	category?: { id: string; name: string; boxArtUrl?: string | null };
	title?: { name: string };
	onMediaAxis?: boolean;
};

export type ContentSegment = {
	key: string;
	startSeconds: number;
	endSeconds: number;
	category?: { id: string; name: string; boxArtUrl?: string | null };
	title?: { name: string };
};

export function metadataMarkers(
	events: TimelineEvent[] | undefined,
	wallClockAnchor: string,
): MetadataMarker[] {
	return timelineChangeEvents(events)
		.map(({ event, changes }): MetadataMarker | null => {
			const label = timelineMarkerLabel(event, changes);
			if (!label) return null;
			const offsetSeconds = timelineEventOffsetSeconds(event, wallClockAnchor);
			if (offsetSeconds < 0) return null;
			return {
				key: timelineEventKey(event),
				offsetSeconds,
				label,
				kind: timelineMarkerKind(changes),
				category:
					changes.includes("category") && event.category
						? {
								id: event.category.id,
								name: event.category.name,
								boxArtUrl: event.category.box_art_url,
							}
						: undefined,
				title:
					changes.includes("title") && event.title
						? { name: event.title.name }
						: undefined,
				onMediaAxis:
					typeof event.media_offset_seconds === "number" &&
					Number.isFinite(event.media_offset_seconds),
			};
		})
		.filter((marker): marker is MetadataMarker => marker != null);
}

export function orderedMetadataMarkers(
	events: TimelineEvent[] | undefined,
	wallClockAnchor: string,
): MetadataMarker[] {
	return sortMetadataMarkers(metadataMarkers(events, wallClockAnchor));
}

export function clampMetadataMarkers(
	markers: MetadataMarker[],
	scaleSeconds: number,
): MetadataMarker[] {
	if (scaleSeconds <= 0) return [];
	return markers
		.map((marker): MetadataMarker | null => {
			if (marker.onMediaAxis && marker.offsetSeconds > scaleSeconds)
				return null;
			return {
				...marker,
				offsetSeconds: Math.min(marker.offsetSeconds, scaleSeconds),
			};
		})
		.filter((marker): marker is MetadataMarker => marker != null);
}

function sortMetadataMarkers(markers: MetadataMarker[]): MetadataMarker[] {
	return [...markers].sort((a, b) => a.offsetSeconds - b.offsetSeconds);
}

function activeMetadataAt(
	ordered: MetadataMarker[],
	atSeconds: number,
): Pick<ContentSegment, "category" | "title"> {
	let category: ContentSegment["category"];
	let title: ContentSegment["title"];
	for (const marker of ordered) {
		if (marker.offsetSeconds > atSeconds) break;
		if (marker.category) category = marker.category;
		if (marker.title) title = marker.title;
	}
	return { category, title };
}

export function contentSegments(
	markers: MetadataMarker[],
	scaleSeconds: number,
): ContentSegment[] {
	return contentSegmentsFromOrderedMarkers(
		sortMetadataMarkers(markers),
		scaleSeconds,
	);
}

export function contentSegmentsFromOrderedMarkers(
	ordered: MetadataMarker[],
	scaleSeconds: number,
): ContentSegment[] {
	if (scaleSeconds <= 0) return [];
	if (ordered.length === 0) {
		return [{ key: "seg-0", startSeconds: 0, endSeconds: scaleSeconds }];
	}
	const segments: ContentSegment[] = [];
	const first = ordered[0];
	if (first && first.offsetSeconds > 0) {
		segments.push({
			key: "seg-init",
			startSeconds: 0,
			endSeconds: Math.min(first.offsetSeconds, scaleSeconds),
		});
	}
	for (let i = 0; i < ordered.length; i++) {
		const marker = ordered[i];
		if (!marker) continue;
		const start = Math.min(marker.offsetSeconds, scaleSeconds);
		const next = ordered[i + 1];
		const end = next
			? Math.min(next.offsetSeconds, scaleSeconds)
			: scaleSeconds;
		if (end <= start) continue;
		segments.push({
			key: `${marker.key}:${i}`,
			startSeconds: start,
			endSeconds: end,
			...activeMetadataAt(ordered, marker.offsetSeconds),
		});
	}
	return segments;
}

export function recordingElapsedSeconds(row: ActiveDownloadResponse): number {
	const mediaOffset = positiveOrZero(row.media_offset_seconds);
	if (mediaOffset > 0) return mediaOffset;
	const partsDuration = orderedVideoParts(row.video).reduce(
		(sum, part) => sum + positiveOrZero(part.duration_seconds),
		0,
	);
	if (partsDuration > 0) return partsDuration;
	return 0;
}
