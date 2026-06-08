import type { QueryClient } from "@tanstack/react-query";
import {
	type CacheGroup,
	type CacheSnapshot,
	cancelCaches,
	invalidateCaches,
	restoreCaches,
	snapshotCaches,
} from "./cache";

// Codifies the optimistic-mutation contract once: cancel + snapshot the families,
// apply the patch, roll back on error, reconcile from the server response, and
// invalidate on settle. Callers supply only apply/applyServer, so a mutation
// can't get the key semantics wrong. Spread into a tRPC mutationOptions() call.
export type OptimisticWriteConfig<TData, TVars> = {
	apply: (qc: QueryClient, vars: TVars) => void;
	applyServer?: (qc: QueryClient, data: TData, vars: TVars) => void;
	invalidateOnly?: readonly string[];
};

type OptimisticContext = { snapshot: CacheSnapshot };

export function optimisticWrite<TData, TVars>(
	qc: QueryClient,
	caches: CacheGroup,
	config: OptimisticWriteConfig<TData, TVars>,
) {
	return {
		onMutate: async (vars: TVars): Promise<OptimisticContext> => {
			await cancelCaches(qc, caches);
			const snapshot = snapshotCaches(qc, caches);
			config.apply(qc, vars);
			return { snapshot };
		},
		onError: (_err: unknown, _vars: TVars, context?: OptimisticContext) => {
			if (context) restoreCaches(qc, context.snapshot);
		},
		onSuccess: (data: TData, vars: TVars) => {
			config.applyServer?.(qc, data, vars);
		},
		onSettled: () => {
			invalidateCaches(qc, caches, config.invalidateOnly);
		},
	};
}
