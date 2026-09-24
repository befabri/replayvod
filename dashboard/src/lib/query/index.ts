export {
	type CacheGroup,
	type CacheShape,
	type CacheSnapshot,
	type CacheSpec,
	cancelCaches,
	defineCaches,
	type EntityPatch,
	invalidateCaches,
	keyHasInput,
	patchEntity,
	restoreCaches,
	resyncQuery,
	snapshotCaches,
} from "./cache";
export { DEFAULT_QUERY_ERROR_LABEL, queryErrorLabel } from "./meta";
export {
	type OptimisticWriteConfig,
	optimisticWrite,
} from "./optimistic";
