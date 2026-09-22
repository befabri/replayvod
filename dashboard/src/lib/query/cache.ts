import type {
	InfiniteData,
	QueryClient,
	QueryKey,
} from "@tanstack/react-query";

export type CacheShape = "single" | "array" | "wrapped" | "infinite" | "scalar";

type ProcedureNode = { pathKey: () => QueryKey; "~types": { output: unknown } };

type NodeOutput<N> = N extends { "~types": { output: infer O } } ? O : never;

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

export type EntityPatch<Row> = {
	match: (row: Row) => boolean;
	update: (row: Row) => Row;
	removeFrom?: (queryKey: QueryKey, shape: CacheShape, row: Row) => boolean;
};

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
			const remove = (row: Row) =>
				patch.removeFrom?.(queryKey, spec.shape, row) ?? false;
			const next = applyShapePatch(spec.shape, data, patch, remove);
			if (next !== data) qc.setQueryData(queryKey, next);
		}
	}
}

function applyShapePatch<Row>(
	shape: CacheShape,
	data: unknown,
	patch: EntityPatch<Row>,
	remove: (row: Row) => boolean,
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
	remove: (row: Row) => boolean,
): Row[] {
	let changed = false;
	const next: Row[] = [];
	for (const row of rows) {
		if (!patch.match(row)) {
			next.push(row);
			continue;
		}
		changed = true;
		const updated = patch.update(row);
		if (remove(updated)) continue;
		next.push(updated);
	}
	return changed ? next : rows;
}

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

export async function resyncQuery(qc: QueryClient, queryKey: QueryKey) {
	const filter = { queryKey };
	await qc.cancelQueries(filter);
	await qc.invalidateQueries(filter);
}
