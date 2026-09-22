import {
	type QueryClient,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import type { SettingsResponse } from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";
import { VIDEO_USER_STATE_CACHES, videoCaches } from "@/features/videos/cache";
import { invalidateCaches } from "@/lib/query";
import { authStore } from "@/stores/auth";

async function cacheSavedSettings(
	queryClient: QueryClient,
	trpc: ReturnType<typeof useTRPC>,
	saved: SettingsResponse,
	changes: Partial<SettingsResponse>,
) {
	if (authStore.state.user?.id !== saved.user_id) return false;
	await queryClient.cancelQueries({ queryKey: trpc.settings.get.pathKey() });
	if (authStore.state.user?.id !== saved.user_id) return false;
	queryClient.setQueryData(trpc.settings.get.queryKey(), (current) => ({
		...(current?.user_id === saved.user_id ? current : saved),
		...changes,
		updated_at: saved.updated_at,
	}));
	return true;
}

export function useSettings() {
	const trpc = useTRPC();
	return useQuery(
		trpc.settings.get.queryOptions(undefined, { staleTime: 60_000 }),
	);
}

export function useUpdatePlaybackSettings() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.settings.updatePlayback.mutationOptions({
			onMutate: () =>
				queryClient.cancelQueries({ queryKey: trpc.settings.get.pathKey() }),
			onSuccess: async (saved) => {
				if (
					!(await cacheSavedSettings(queryClient, trpc, saved, {
						playback: saved.playback,
					}))
				)
					return;
				invalidateCaches(
					queryClient,
					videoCaches(trpc),
					VIDEO_USER_STATE_CACHES,
				);
			},
		}),
	);
}

export function useUpdateSettings() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.settings.update.mutationOptions({
			onMutate: () =>
				queryClient.cancelQueries({ queryKey: trpc.settings.get.pathKey() }),
			onSuccess: async (saved) => {
				await cacheSavedSettings(queryClient, trpc, saved, {
					timezone: saved.timezone,
					datetime_format: saved.datetime_format,
					language: saved.language,
				});
			},
		}),
	);
}
