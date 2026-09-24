import { QueryBoundary } from "@/components/query-boundary";
import { ContinueWatching } from "./ContinueWatching";
import { LatestRecordings, LatestRecordingsSkeleton } from "./LatestRecordings";

export function HomeVideoSections() {
	return (
		<QueryBoundary fallback={<LatestRecordingsSkeleton />}>
			<LatestRecordings />
			<ContinueWatching />
		</QueryBoundary>
	);
}
