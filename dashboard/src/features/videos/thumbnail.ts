import { API_URL } from "@/env";

// Stored posters and snapshots may include the storage directory prefix.
export function localThumbnailURL(path: string): string {
	return `${API_URL}/api/v1/thumbnails/${path.replace(/^thumbnails\//, "")}`;
}
