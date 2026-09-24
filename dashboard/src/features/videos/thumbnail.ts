import type { VideoResponse } from "@/api/generated/trpc";
import { API_URL } from "@/env";

export function localThumbnailURL(path: string): string {
	return `${API_URL}/api/v1/thumbnails/${path.replace(/^thumbnails\//, "")}`;
}

export function firstSnapshotPath(filename: string): string {
	return `thumbnails/${filename}-snap00.jpg`;
}

export function recordingPosterURL(
	video: Pick<VideoResponse, "thumbnail">,
): string | null {
	return video.thumbnail ? localThumbnailURL(video.thumbnail) : null;
}
