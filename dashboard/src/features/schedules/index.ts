export type { ScheduleResponse } from "@/api/generated/trpc";
export {
	useCreateSchedule,
	useDeleteSchedule,
	useMineSchedules,
	useSchedules,
	useSchedulesPaused,
	useSetSchedulesPaused,
	useToggleSchedule,
	useUpdateSchedule,
} from "./queries";
