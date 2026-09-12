import type { QueryState } from "@tanstack/react-query";
import { z } from "zod";
import type { LiveRenditionsResponse } from "@/api/generated/trpc";
import {
	forceH264For,
	qualityTierForHeight,
	RECORDING_QUALITY_HEIGHT,
	type RecordingQuality,
	RecordingQualitySchema,
} from "@/lib/recording-settings";
import {
	type RenditionOption,
	renditionAtOrBelow,
	renditionOptions,
} from "./renditions";

// Store the user's preference, never a default copied from a query result.
// A ladder tier is a ceiling; a number is a height explicitly picked from
// the live list. These are alternatives, so they cannot drift out of sync.
export const DirectDownloadFormSchema = z.object({
	recording_type: z.enum(["video", "audio"]),
	quality: z.union([RecordingQualitySchema, z.number().int().min(1).max(4320)]),
	force_h264: z.boolean(),
});

export type DirectDownloadFormValues = z.infer<typeof DirectDownloadFormSchema>;
export type DirectDownloadAvailability =
	| "checking"
	| "live"
	| "offline"
	| "error";

// A cached live verdict cannot authorize a submit while it is being rechecked.
// Errors remain distinct from an authoritative offline response.
export function resolveDirectDownloadAvailability(
	snapshot:
		| Pick<QueryState<boolean>, "data" | "status" | "fetchStatus">
		| undefined,
): DirectDownloadAvailability {
	if (
		!snapshot ||
		snapshot.status === "pending" ||
		snapshot.fetchStatus !== "idle"
	) {
		return "checking";
	}
	if (snapshot.status === "error") return "error";
	return snapshot.data === true ? "live" : "offline";
}

export type DirectDownloadQuality =
	| { kind: "loading" }
	| { kind: "ceiling"; quality: RecordingQuality }
	| {
			kind: "rendition";
			options: RenditionOption[];
			height: number | null;
			anonymous: boolean;
	  };

type RenditionsSnapshot = Pick<
	QueryState<LiveRenditionsResponse>,
	"data" | "status" | "fetchStatus"
>;

// Render and submission resolve the same preference against the same query
// identity. No effect writes derived heights back into the user's form.
export function resolveDirectDownloadQuality(
	values: DirectDownloadFormValues,
	lookupEnabled: boolean,
	snapshot: RenditionsSnapshot | undefined,
): DirectDownloadQuality {
	const quality =
		typeof values.quality === "number"
			? qualityTierForHeight(values.quality)
			: values.quality;
	if (!lookupEnabled || values.recording_type === "audio") {
		return { kind: "ceiling", quality };
	}
	const options = renditionOptions(
		snapshot?.data?.renditions ?? [],
		values.force_h264,
	);
	if (
		!snapshot ||
		snapshot.status === "pending" ||
		(options.length === 0 && snapshot.fetchStatus !== "idle")
	) {
		return { kind: "loading" };
	}
	if (options.length === 0) return { kind: "ceiling", quality };
	return {
		kind: "rendition",
		options,
		height: renditionAtOrBelow(
			options,
			typeof values.quality === "number"
				? values.quality
				: RECORDING_QUALITY_HEIGHT[values.quality],
		),
		anonymous: snapshot.data?.anonymous ?? false,
	};
}

// Numeric quality is already resolved to a visible live rendition before
// submission. The server receives its exact height and its display tier.
export function buildDirectDownloadPayload(
	broadcasterId: string,
	value: DirectDownloadFormValues,
) {
	const pinned =
		value.recording_type === "video" && typeof value.quality === "number"
			? value.quality
			: null;
	return {
		broadcaster_id: broadcasterId,
		recording_type: value.recording_type,
		quality:
			typeof value.quality === "number"
				? qualityTierForHeight(value.quality)
				: value.quality,
		force_h264: forceH264For(value.recording_type, value.force_h264),
		...(pinned === null ? {} : { max_height: pinned }),
	};
}
