import type {
	StorageDetailsResponse,
	StorageState,
} from "@/api/generated/trpc";
import { FIXTURE_NOW } from "./videos";

export function makeStorageDetails(
	state: StorageState = "attached",
	overrides: Partial<StorageDetailsResponse> = {},
): StorageDetailsResponse {
	return {
		state,
		reason: state === "attached" ? "" : "the marker file is missing",
		backend: "local",
		location: "/srv/replayvod/recordings",
		storage_id: "3f9c2a7e-58d1-4c0b-9b6e-2d41f0a8c7e5",
		checked_at: new Date(FIXTURE_NOW - 90_000).toISOString(),
		...overrides,
	};
}
