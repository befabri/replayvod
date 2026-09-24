export function startClockAt(now: number): () => void {
	const RealDate = globalThis.Date;
	const offset = now - RealDate.now();
	const shiftedNow = () => RealDate.now() + offset;

	globalThis.Date = new Proxy(RealDate, {
		construct: (target, args, newTarget) =>
			Reflect.construct(
				target,
				args.length > 0 ? args : [shiftedNow()],
				newTarget,
			),
		apply: () => new RealDate(shiftedNow()).toString(),
		get: (target, property, receiver) =>
			property === "now" ? shiftedNow : Reflect.get(target, property, receiver),
	});

	return () => {
		globalThis.Date = RealDate;
	};
}
