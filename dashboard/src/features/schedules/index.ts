export type { ScheduleResponse } from "@/api/generated/trpc";
export {
	useCreateSchedule,
	useDeleteSchedule,
	useMineSchedules,
	useSchedules,
	useToggleSchedule,
	useUpdateSchedule,
} from "./queries";
