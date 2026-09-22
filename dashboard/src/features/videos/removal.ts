import type { VideoResponse } from "@/api/generated/trpc";

type RemovalRecord = Pick<
	VideoResponse,
	"status" | "deleted_at" | "deletion_kind" | "delete_requested_at"
>;

export function videoRemovalState(video: RemovalRecord) {
	if (video.status !== "DONE" && video.status !== "FAILED")
		return "unavailable";
	if (video.delete_requested_at) return "pending";
	if (!video.deleted_at) return "removable";
	return video.deletion_kind === "missing" ? "restorable" : "removed";
}
