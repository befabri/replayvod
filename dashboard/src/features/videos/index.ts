export type { VideoResponse, StatisticsResponse } from "@/api/generated/trpc"
export {
	useVideos,
	useVideo,
	useVideosByBroadcaster,
	useStatistics,
	useTriggerDownload,
	useCancelDownload,
} from "./queries"
