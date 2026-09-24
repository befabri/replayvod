import type { ResumePolicy } from "@/features/videos/resume-policy";
import { useSuspenseSettings } from "./queries";

export function usePlaybackSettings(): ResumePolicy {
	return useSuspenseSettings().data.playback;
}
