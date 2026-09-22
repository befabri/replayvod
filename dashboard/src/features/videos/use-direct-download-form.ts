import { useForm } from "@tanstack/react-form";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSelector } from "@tanstack/react-store";
import type { TriggerDownloadInput } from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";
import { broadcasterLiveOptions } from "@/features/streams-live/queries";
import {
	buildDirectDownloadPayload,
	DirectDownloadFormSchema,
	type DirectDownloadFormValues,
	resolveDirectDownloadAvailability,
	resolveDirectDownloadQuality,
} from "./download-form";
import { liveRenditionsOptions } from "./queries";

export function useDirectDownloadForm({
	broadcasterId,
	disabled = false,
	onSubmit,
}: {
	broadcasterId: string;
	disabled?: boolean;
	onSubmit: (payload: TriggerDownloadInput) => Promise<void>;
}) {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const canLookup = !disabled && !!broadcasterId;
	const liveOptions = broadcasterLiveOptions(trpc, broadcasterId);
	const live = useQuery({ ...liveOptions, enabled: canLookup });
	const availability = resolveDirectDownloadAvailability(live);
	const form = useForm({
		defaultValues: {
			recording_type: "video",
			quality: "HIGH",
			force_h264: false,
		} as DirectDownloadFormValues,
		validators: { onSubmit: DirectDownloadFormSchema },
		onSubmit: async ({ value }) => {
			if (disabled || !broadcasterId) return;
			if (
				resolveDirectDownloadAvailability(
					queryClient.getQueryState(liveOptions.queryKey),
				) !== "live"
			)
				return;
			const snapshot = queryClient.getQueryState(
				liveRenditionsOptions(trpc, broadcasterId, value.force_h264).queryKey,
			);
			const quality = resolveDirectDownloadQuality(value, canLookup, snapshot);
			if (quality.kind === "loading") return;
			const selected =
				quality.kind === "rendition" ? quality.height : quality.quality;
			if (selected === null) return;
			await onSubmit(
				buildDirectDownloadPayload(broadcasterId, {
					...value,
					quality: selected,
				}),
			);
		},
	});
	const values = useSelector(form.store, (state) => state.values);
	const renditions = useQuery({
		...liveRenditionsOptions(trpc, broadcasterId, values.force_h264),
		enabled:
			canLookup && availability === "live" && values.recording_type === "video",
	});
	const quality = resolveDirectDownloadQuality(values, canLookup, renditions);
	const ready =
		!disabled &&
		!!broadcasterId &&
		availability === "live" &&
		quality.kind !== "loading" &&
		(quality.kind === "rendition"
			? quality.height !== null
			: quality.quality !== null);

	return {
		form,
		values,
		quality,
		ready,
		availability,
		disabled: disabled || availability !== "live",
		recheck: () => {
			void live.refetch();
		},
	};
}

export type DirectDownloadController = ReturnType<typeof useDirectDownloadForm>;
