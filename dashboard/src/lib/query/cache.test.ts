import {
	type InfiniteData,
	QueryClient,
	type QueryKey,
} from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import {
	type CacheGroup,
	cancelCaches,
	defineCaches,
	invalidateCaches,
	keyHasInput,
	patchEntity,
	restoreCaches,
	type ShapeFor,
	snapshotCaches,
} from "./cache";
import { optimisticWrite } from "./optimistic";

type Row = { id: number; name: string };

// Fake a tRPC query node carrying the ~types.output phantom, so defineCaches'
// shape check sees a realistic output type for each shape under test.
function node<Output>(...path: string[]) {
	return { pathKey: () => [path] } as unknown as {
		pathKey: () => QueryKey;
		"~types": { output: Output };
	};
}

function widgetCaches(): CacheGroup {
	return defineCaches({
		one: { path: node<Row>("widget", "one"), shape: "single" },
		list: { path: node<Row[]>("widget", "list"), shape: "array" },
		wrapped: {
			path: node<{ data: Row[] }>("widget", "wrapped"),
			shape: "wrapped",
		},
		pages: {
			path: node<{ items: Row[] }>("widget", "pages"),
			shape: "infinite",
		},
		stat: { path: node<{ count: number }>("widget", "stat"), shape: "scalar" },
	});
}

const upper = {
	match: (row: Row) => row.id === 1,
	update: (row: Row) => ({ ...row, name: row.name.toUpperCase() }),
};

describe("defineCaches", () => {
	it("resolves pathKeys and carries shapes", () => {
		const caches = widgetCaches();
		expect(caches.list).toEqual({
			pathKey: [["widget", "list"]],
			shape: "array",
		});
		expect(caches.pages.shape).toBe("infinite");
	});

	it("constrains shape to the procedure output (compile-time)", () => {
		// A { items: [] } output is infinite; "scalar" is the always-allowed opt-out.
		const accept = <O>(_shape: ShapeFor<O>) => true;
		expect(accept<{ items: Row[] }>("infinite")).toBe(true);
		expect(accept<{ items: Row[] }>("scalar")).toBe(true);
		expect(accept<Row[]>("array")).toBe(true);
		expect(accept<{ data: Row[] }>("wrapped")).toBe(true);
		// @ts-expect-error "wrapped" is invalid for an { items: [] } (infinite) output
		expect(accept<{ items: Row[] }>("wrapped")).toBe(true);
		// @ts-expect-error "array" is invalid for a single-object output
		expect(accept<{ id: number }>("array")).toBe(true);
	});
});

describe("patchEntity", () => {
	it("patches array, wrapped, infinite and single shapes", () => {
		const qc = new QueryClient();
		const caches = widgetCaches();
		qc.setQueryData(caches.one.pathKey, { id: 1, name: "a" });
		qc.setQueryData(caches.list.pathKey, [
			{ id: 1, name: "a" },
			{ id: 2, name: "b" },
		]);
		qc.setQueryData(caches.wrapped.pathKey, {
			data: [{ id: 1, name: "a" }],
		});
		qc.setQueryData(caches.pages.pathKey, {
			pages: [{ items: [{ id: 1, name: "a" }] }],
			pageParams: [undefined],
		} satisfies InfiniteData<{ items: Row[] }>);

		patchEntity<Row>(qc, caches, upper);

		expect(qc.getQueryData(caches.one.pathKey)).toEqual({ id: 1, name: "A" });
		expect(qc.getQueryData(caches.list.pathKey)).toEqual([
			{ id: 1, name: "A" },
			{ id: 2, name: "b" },
		]);
		expect(qc.getQueryData(caches.wrapped.pathKey)).toEqual({
			data: [{ id: 1, name: "A" }],
		});
		const pages = qc.getQueryData<InfiniteData<{ items: Row[] }>>(
			caches.pages.pathKey,
		);
		expect(pages?.pages[0]?.items[0]?.name).toBe("A");
	});

	it("never touches scalar caches", () => {
		const qc = new QueryClient();
		const caches = widgetCaches();
		const stat = { count: 7 };
		qc.setQueryData(caches.stat.pathKey, stat);

		patchEntity<Row>(qc, caches, {
			match: () => true,
			update: (row) => row,
		});

		expect(qc.getQueryData(caches.stat.pathKey)).toBe(stat);
	});

	it("drops matched rows from caches removeFrom selects", () => {
		const qc = new QueryClient();
		const caches = widgetCaches();
		qc.setQueryData(caches.list.pathKey, [
			{ id: 1, name: "a" },
			{ id: 2, name: "b" },
		]);

		patchEntity<Row>(qc, caches, {
			...upper,
			removeFrom: (_key, shape) => shape === "array",
		});

		expect(qc.getQueryData(caches.list.pathKey)).toEqual([
			{ id: 2, name: "b" },
		]);
	});

	it("preserves referential identity when nothing matches", () => {
		const qc = new QueryClient();
		const caches = widgetCaches();
		const list = [{ id: 9, name: "x" }];
		qc.setQueryData(caches.list.pathKey, list);

		patchEntity<Row>(qc, caches, upper);

		expect(qc.getQueryData(caches.list.pathKey)).toBe(list);
	});
});

describe("snapshot / restore", () => {
	it("rolls a patched cache back to its captured state", () => {
		const qc = new QueryClient();
		const caches = widgetCaches();
		qc.setQueryData(caches.list.pathKey, [{ id: 1, name: "a" }]);

		const snapshot = snapshotCaches(qc, caches);
		patchEntity<Row>(qc, caches, upper);
		expect(qc.getQueryData(caches.list.pathKey)).toEqual([
			{ id: 1, name: "A" },
		]);

		restoreCaches(qc, snapshot);
		expect(qc.getQueryData(caches.list.pathKey)).toEqual([
			{ id: 1, name: "a" },
		]);
	});
});

describe("invalidateCaches", () => {
	it("invalidates the whole group, or just the named subset", () => {
		const qc = new QueryClient();
		const caches = widgetCaches();
		const spy = vi.spyOn(qc, "invalidateQueries");

		invalidateCaches(qc, caches, ["list", "one"]);
		expect(spy).toHaveBeenCalledTimes(2);
		expect(spy).toHaveBeenCalledWith({ queryKey: caches.list.pathKey });
		expect(spy).toHaveBeenCalledWith({ queryKey: caches.one.pathKey });

		spy.mockClear();
		invalidateCaches(qc, caches);
		expect(spy).toHaveBeenCalledTimes(Object.keys(caches).length);
	});
});

describe("keyHasInput", () => {
	it("finds a field/value pair nested in a resolved tRPC key", () => {
		const key = [
			["widget", "pages"],
			{ input: { filter: "favorites" }, type: "infinite" },
		];
		expect(keyHasInput(key, "filter", "favorites")).toBe(true);
		expect(keyHasInput(key, "filter", "downloaded")).toBe(false);
	});
});

describe("optimisticWrite", () => {
	it("applies on mutate and reconciles on success", async () => {
		const qc = new QueryClient();
		const caches = widgetCaches();
		qc.setQueryData(caches.list.pathKey, [{ id: 1, name: "a" }]);

		const handlers = optimisticWrite<{ name: string }, { id: number }>(
			qc,
			caches,
			{
				apply: (client) =>
					patchEntity<Row>(client, caches, {
						match: (row) => row.id === 1,
						update: (row) => ({ ...row, name: "optimistic" }),
					}),
				applyServer: (client, data) =>
					patchEntity<Row>(client, caches, {
						match: (row) => row.id === 1,
						update: (row) => ({ ...row, name: data.name }),
					}),
			},
		);

		const ctx = await handlers.onMutate({ id: 1 });
		expect(qc.getQueryData(caches.list.pathKey)).toEqual([
			{ id: 1, name: "optimistic" },
		]);
		expect(ctx.snapshot.length).toBeGreaterThan(0);

		handlers.onSuccess({ name: "server" }, { id: 1 });
		expect(qc.getQueryData(caches.list.pathKey)).toEqual([
			{ id: 1, name: "server" },
		]);
	});

	it("rolls back on error", async () => {
		const qc = new QueryClient();
		const caches = widgetCaches();
		qc.setQueryData(caches.list.pathKey, [{ id: 1, name: "a" }]);

		const handlers = optimisticWrite<unknown, { id: number }>(qc, caches, {
			apply: (client) =>
				patchEntity<Row>(client, caches, {
					match: (row) => row.id === 1,
					update: (row) => ({ ...row, name: "optimistic" }),
				}),
		});

		const ctx = await handlers.onMutate({ id: 1 });
		handlers.onError(new Error("boom"), { id: 1 }, ctx);
		expect(qc.getQueryData(caches.list.pathKey)).toEqual([
			{ id: 1, name: "a" },
		]);
	});

	it("cancels in-flight queries before snapshotting", async () => {
		const qc = new QueryClient();
		const caches = widgetCaches();
		const cancel = vi.spyOn(qc, "cancelQueries").mockResolvedValue();

		await cancelCaches(qc, caches);
		expect(cancel).toHaveBeenCalledTimes(Object.keys(caches).length);
	});
});
