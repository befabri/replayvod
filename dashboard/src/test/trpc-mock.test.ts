import { describe, expect, it, vi } from "vitest";
import { makeActiveDownload } from "./fixtures";
import {
	createMockTrpcClient,
	neverResolves,
	type TrpcHandlers,
	trpcError,
} from "./trpc-mock";

const DOWNLOADS = [makeActiveDownload()];

function stalledFeed() {
	const returned = vi.fn(async () => ({
		done: true as const,
		value: undefined,
	}));
	let delivered = false;
	const iterable: AsyncIterable<typeof DOWNLOADS> = {
		[Symbol.asyncIterator]: () => ({
			next: () => {
				if (delivered) return neverResolves();
				delivered = true;
				return Promise.resolve({ done: false, value: DOWNLOADS });
			},
			return: returned,
		}),
	};
	return { iterable, returned };
}

function subscribe(handlers: TrpcHandlers) {
	const onData = vi.fn();
	const onStopped = vi.fn();
	const onComplete = vi.fn();
	const onError = vi.fn();
	const subscription = createMockTrpcClient(
		handlers,
	).video.activeDownloadsLive.subscribe(undefined, {
		onData,
		onStopped,
		onComplete,
		onError,
	});
	return { subscription, onData, onStopped, onComplete, onError };
}

describe("mock tRPC link", () => {
	it("resolves queries with the handler output", async () => {
		const client = createMockTrpcClient({
			tag: { list: () => [{ id: 1, name: "Chill", created_at: "" }] },
		});
		await expect(client.tag.list.query()).resolves.toEqual([
			{ id: 1, name: "Chill", created_at: "" },
		]);
	});

	it("fails a procedure that has no handler and names it", async () => {
		const client = createMockTrpcClient({});
		await expect(client.tag.list.query()).rejects.toThrow(
			"No tRPC mock handler for tag.list",
		);
	});

	it("forwards trpcError codes to the caller", async () => {
		const client = createMockTrpcClient({
			tag: {
				list: () => {
					throw trpcError("FORBIDDEN", 403);
				},
			},
		});
		await expect(client.tag.list.query()).rejects.toMatchObject({
			data: { code: "FORBIDDEN", httpStatus: 403 },
		});
	});

	it("streams every value of a finite subscription, then stops", async () => {
		const { onData, onStopped, onComplete } = subscribe({
			video: {
				activeDownloadsLive: async function* () {
					yield DOWNLOADS;
					yield [];
				},
			},
		});
		await vi.waitFor(() => expect(onComplete).toHaveBeenCalled());
		expect(onData.mock.calls).toEqual([[DOWNLOADS], [[]]]);
		expect(onStopped).toHaveBeenCalled();
	});

	it("closes the handler's iterator when the subscriber leaves", async () => {
		const feed = stalledFeed();
		const { subscription, onData, onError } = subscribe({
			video: { activeDownloadsLive: () => feed.iterable },
		});
		await vi.waitFor(() => expect(onData).toHaveBeenCalledWith(DOWNLOADS));
		subscription.unsubscribe();
		expect(feed.returned).toHaveBeenCalledOnce();
		expect(onError).not.toHaveBeenCalled();
	});

	it("closes an iterator that arrives after the subscriber left", async () => {
		const feed = stalledFeed();
		let resolveHandler: (value: AsyncIterable<typeof DOWNLOADS>) => void =
			() => {};
		const { subscription } = subscribe({
			video: {
				activeDownloadsLive: () =>
					new Promise((resolve) => {
						resolveHandler = resolve;
					}),
			},
		});
		subscription.unsubscribe();
		resolveHandler(feed.iterable);
		await vi.waitFor(() => expect(feed.returned).toHaveBeenCalledOnce());
	});
});
