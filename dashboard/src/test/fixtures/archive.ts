import type { EnqueueArchiveItem } from "@/api/generated/trpc";
import { VOD_TITLES } from "./videos";

export function vodLink(id: number): string {
	return `https://www.twitch.tv/videos/${id}`;
}

export function vodLinks(count: number, firstId = 2_000_000_101): string[] {
	return Array.from({ length: count }, (_, index) => vodLink(firstId + index));
}

export function makeEnqueueResults(
	links: readonly string[],
): EnqueueArchiveItem[] {
	return links.map((input, index) => {
		const vodId = input.split("/").pop() ?? input;
		if (index % 3 === 1) {
			return {
				input,
				vod_id: vodId,
				status: "exists",
				title: VOD_TITLES[index % VOD_TITLES.length],
				video_id: 100 + index,
			};
		}
		if (index % 3 === 2) {
			return { input, vod_id: vodId, status: "not_found", message: "gone" };
		}
		return {
			input,
			vod_id: vodId,
			status: "queued",
			title: VOD_TITLES[index % VOD_TITLES.length],
			video_id: 200 + index,
			job_id: `job-${200 + index}`,
		};
	});
}
