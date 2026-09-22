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
	| { kind: "ceiling"; quality: RecordingQuality | null }
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
	if (options.length === 0) {
		return {
			kind: "ceiling",
			quality:
				typeof values.quality === "number" &&
				RECORDING_QUALITY_HEIGHT[quality] !== values.quality
					? null
					: quality,
		};
	}
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
