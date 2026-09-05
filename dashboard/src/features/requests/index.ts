export type {
	ScheduleRequestResponse,
	ScheduleRequestStatus,
} from "@/api/generated/trpc";
export {
	useAllScheduleRequests,
	useApproveScheduleRequest,
	useCancelScheduleRequest,
	useCreateScheduleRequest,
	useMyScheduleRequests,
	useRejectScheduleRequest,
} from "./queries";
