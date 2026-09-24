import { createTRPCClient, TRPCClientError, type TRPCLink } from "@trpc/client";
import { observable } from "@trpc/server/observable";
import type { AppRouter, RouterInputs, RouterOutputs } from "@/api/trpc";

type Resolved<T> = T | AsyncIterable<T> | Promise<T | AsyncIterable<T>>;

export type TrpcHandlers = {
	[R in keyof RouterOutputs]?: {
		[P in keyof RouterOutputs[R]]?: (
			input: RouterInputs[R][P],
		) => Resolved<RouterOutputs[R][P]>;
	};
};

type AnyHandler = (input: unknown) => unknown;
type HandlerGroups = Record<string, Record<string, AnyHandler> | undefined>;

export function mergeTrpcHandlers(...layers: TrpcHandlers[]): TrpcHandlers {
	const merged: HandlerGroups = {};
	for (const layer of layers as HandlerGroups[]) {
		for (const [router, procedures] of Object.entries(layer)) {
			merged[router] = { ...merged[router], ...procedures };
		}
	}
	return merged as TrpcHandlers;
}

export function trpcParameters(handlers: TrpcHandlers) {
	return { trpc: handlers };
}

export function neverResolves(): Promise<never> {
	return new Promise<never>(() => {});
}

export function trpcError(code: string, httpStatus: number, message = code) {
	return TRPCClientError.from({
		error: { message, code: -32000, data: { code, httpStatus } },
	});
}

function findHandler(
	handlers: TrpcHandlers,
	path: string,
): AnyHandler | undefined {
	const [router, procedure] = path.split(".");
	return (handlers as HandlerGroups)[router]?.[procedure];
}

function isAsyncIterable(value: unknown): value is AsyncIterable<unknown> {
	return (
		typeof value === "object" && value !== null && Symbol.asyncIterator in value
	);
}

function closeIterator(iterator: AsyncIterator<unknown>) {
	void iterator.return?.()?.catch(() => undefined);
}

function toClientError(cause: unknown) {
	if (cause instanceof TRPCClientError) return cause;
	return TRPCClientError.from(
		cause instanceof Error ? cause : new Error(String(cause)),
	);
}

function mockLink(handlers: TrpcHandlers): TRPCLink<AppRouter> {
	return () =>
		({ op }) =>
			observable((observer) => {
				const handler = findHandler(handlers, op.path);
				if (!handler) {
					observer.error(toClientError(`No tRPC mock handler for ${op.path}`));
					return;
				}
				let active = true;
				let iterator: AsyncIterator<unknown> | undefined;
				const subscription = op.type === "subscription";
				if (subscription) observer.next({ result: { type: "started" } });
				void (async () => {
					try {
						const value = await handler(op.input);
						if (!isAsyncIterable(value)) {
							if (!active) return;
							observer.next({ result: { type: "data", data: value } });
							if (!subscription) observer.complete();
							return;
						}
						iterator = value[Symbol.asyncIterator]();
						if (!active) {
							closeIterator(iterator);
							return;
						}
						for (;;) {
							const step = await iterator.next();
							if (!active) return;
							if (step.done) break;
							observer.next({ result: { type: "data", data: step.value } });
						}
						if (subscription) observer.next({ result: { type: "stopped" } });
						observer.complete();
					} catch (cause) {
						if (active) observer.error(toClientError(cause));
					}
				})();
				return () => {
					active = false;
					if (iterator) closeIterator(iterator);
				};
			});
}

export function createMockTrpcClient(handlers: TrpcHandlers) {
	return createTRPCClient<AppRouter>({ links: [mockLink(handlers)] });
}
