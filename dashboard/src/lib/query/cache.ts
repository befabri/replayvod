import type {
	InfiniteData,
	QueryClient,
	QueryKey,
} from "@tanstack/react-query";

// shape tells the engine each cache's container so it knows how to patch it:
//   single   - the cache value IS one entity row (e.g. getById)
//   array    - a flat Row[] (e.g. search, listFollowed)
//   wrapped  - a { data: Row[] } envelope (e.g. schedule.list)
//   infinite - InfiniteData whose pages are { items: Row[] } (cursor grids)
//   scalar   - a non-row value invalidated/snapshotted but never row-patched
//
// Every family operation keys off pathKey(), so it matches a cache regardless of
// input or query type; queryKey()/infiniteQueryKey() are only for exact get/set.
export type CacheShape = "single" | "array" | "wrapped" | "infinite" | "scalar";

// Only pathKey() runs at runtime; the phantom ~types.output (the marker tRPC's
// own inferOutput reads) lets defineCaches check shape against the real output.
type ProcedureNode = { pathKey: () => QueryKey; "~types": { output: unknown } };

type NodeOutput<N> = N extends { "~types": { output: infer O } } ? O : never;

// A row-list output forces its container, so a wrong infinite/wrapped/array is a
// compile error rather than a runtime crash. "scalar" is the always-allowed
// opt-out for caches that are only invalidated, never row-patched.
type StructuralShape<Output> = [Output] extends [readonly unknown[]]
	? "array"
	: [Output] extends [{ data: readonly unknown[] }]
		? "wrapped"
		: [Output] extends [{ items: readonly unknown[] }]
			? "infinite"
			: "single";
export type ShapeFor<Output> = StructuralShape<Output> | "scalar";

export type CacheSpec = { pathKey: QueryKey; shape: CacheShape };
export type CacheGroup = Record<string, CacheSpec>;

export function defineCaches<
	T extends Record<string, { path: ProcedureNode; shape: CacheShape }>,
>(
	specs: T & { [K in keyof T]: { shape: ShapeFor<NodeOutput<T[K]["path"]>> } },
): { [K in keyof T]: CacheSpec } {
	const out = {} as { [K in keyof T]: CacheSpec };
	for (const key in specs) {
		out[key] = { pathKey: specs[key].path.pathKey(), shape: specs[key].shape };
	}
	return out;
}

export function cancelCaches(qc: QueryClient, caches: CacheGroup) {
	return Promise.all(
		Object.values(caches).map((spec) =>
			qc.cancelQueries({ queryKey: spec.pathKey }),
		),
	);
}

export type CacheSnapshot = Array<[QueryKey, unknown]>;

export function snapshotCaches(
	qc: QueryClient,
	caches: CacheGroup,
): CacheSnapshot {
	return Object.values(caches).flatMap((spec) =>
		qc.getQueriesData({ queryKey: spec.pathKey }),
	);
}

export function restoreCaches(qc: QueryClient, snapshot: CacheSnapshot) {
	for (const [queryKey, data] of snapshot) {
		qc.setQueryData(queryKey, data);
	}
}

// only is keyed off the descriptor, so a typo'd or renamed family name is a
// compile error rather than a silently dropped invalidation.
export function invalidateCaches<T extends CacheGroup>(
	qc: QueryClient,
	caches: T,
	only?: readonly (keyof T & string)[],
) {
	const specs = only ? only.map((name) => caches[name]) : Object.values(caches);
	for (const spec of specs) {
		qc.invalidateQueries({ queryKey: spec.pathKey });
	}
}

// match finds the affected row(s); update returns the patched row; removeFrom
// optionally drops a matched row from a filtered cache (e.g. favorites-only).
export type EntityPatch<Row> = {
	match: (row: Row) => boolean;
	update: (row: Row) => Row;
	removeFrom?: (queryKey: QueryKey, shape: CacheShape) => boolean;
};

// Patch every row-bearing cache via getQueriesData(pathKey) (matches normal and
// infinite alike), writing back only entries that changed so identity is stable.
export function patchEntity<Row>(
	qc: QueryClient,
	caches: CacheGroup,
	patch: EntityPatch<Row>,
) {
	for (const spec of Object.values(caches)) {
		if (spec.shape === "scalar") continue;
		for (const [queryKey, data] of qc.getQueriesData({
			queryKey: spec.pathKey,
		})) {
			const remove = patch.removeFrom?.(queryKey, spec.shape) ?? false;
			const next = applyShapePatch(spec.shape, data, patch, remove);
			if (next !== data) qc.setQueryData(queryKey, next);
		}
	}
}

function applyShapePatch<Row>(
	shape: CacheShape,
	data: unknown,
	patch: EntityPatch<Row>,
	remove: boolean,
): unknown {
	if (data == null) return data;
	switch (shape) {
		case "single": {
			const row = data as Row;
			return patch.match(row) ? patch.update(row) : data;
		}
		case "array": {
			const rows = patchRows(data as Row[], patch, remove);
			return rows;
		}
		case "wrapped": {
			const envelope = data as { data: Row[] };
			const rows = patchRows(envelope.data, patch, remove);
			return rows === envelope.data ? data : { ...envelope, data: rows };
		}
		case "infinite": {
			const infinite = data as InfiniteData<{ items: Row[] }>;
			let changed = false;
			const pages = infinite.pages.map((page) => {
				const items = patchRows(page.items, patch, remove);
				if (items === page.items) return page;
				changed = true;
				return { ...page, items };
			});
			return changed ? { ...infinite, pages } : data;
		}
		default:
			return data;
	}
}

function patchRows<Row>(
	rows: Row[],
	patch: EntityPatch<Row>,
	remove: boolean,
): Row[] {
	let changed = false;
	const next: Row[] = [];
	for (const row of rows) {
		if (!patch.match(row)) {
			next.push(row);
			continue;
		}
		changed = true;
		if (remove) continue;
		next.push(patch.update(row));
	}
	return changed ? next : rows;
}

// Reports whether a resolved tRPC key encodes field === value, so removeFrom can
// detect a filtered cache (e.g. filter:"favorites", watch_later_only:true).
export function keyHasInput(
	queryKey: QueryKey,
	field: string,
	value: unknown,
): boolean {
	return walkForInput(queryKey, field, value);
}

function walkForInput(node: unknown, field: string, value: unknown): boolean {
	if (Array.isArray(node)) {
		return node.some((part) => walkForInput(part, field, value));
	}
	if (!node || typeof node !== "object") return false;
	const record = node as Record<string, unknown>;
	if (field in record && record[field] === value) return true;
	return Object.values(record).some((part) => walkForInput(part, field, value));
}
