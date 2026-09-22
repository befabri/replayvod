import { API_URL } from "@/env";

export function localThumbnailURL(path: string): string {
	return `${API_URL}/api/v1/thumbnails/${path.replace(/^thumbnails\//, "")}`;
}
