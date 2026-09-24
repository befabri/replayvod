import type { Page } from "@playwright/test";

type Landmark = { key: string; x: number; y: number };

// watchLandmarks records, on every animation frame from the first paint, where
// each landmark sits. `byText` elements are keyed by their text, so a
// placeholder and the loaded element that share a label count as one;
// `byOrder` elements are keyed by their position in the page, for boxes whose
// content changes when the data arrives.
export async function watchLandmarks(
	page: Page,
	{ byText = [], byOrder = [] }: { byText?: string[]; byOrder?: string[] },
) {
	await page.addInitScript(
		({ textual, ordered }: { textual: string[]; ordered: string[] }) => {
			const frames: Landmark[][] = [];
			Object.assign(window, { __landmarkFrames: frames });
			const visible = (element: Element) => {
				const box = element.getBoundingClientRect();
				return box.width > 1 && box.height > 1;
			};
			const settled = (element: Element) =>
				!element
					.getAnimations({ subtree: false })
					.some((animation) => animation.playState === "running");
			const place = (key: string, element: Element) => {
				const box = element.getBoundingClientRect();
				return {
					key: `${location.pathname} ${key}`,
					x: Math.round(box.x),
					y: Math.round(box.y + window.scrollY),
				};
			};
			const sample = () => {
				const frame: Landmark[] = [];
				for (const selector of textual) {
					const seen = new Map<string, number>();
					for (const element of document.querySelectorAll(selector)) {
						if (!visible(element) || !settled(element)) continue;
						const text = (element.textContent ?? "").trim().slice(0, 40);
						const nth = seen.get(text) ?? 0;
						seen.set(text, nth + 1);
						frame.push(place(`${selector} "${text}" #${nth}`, element));
					}
				}
				for (const selector of ordered) {
					[...document.querySelectorAll(selector)]
						.filter(visible)
						.forEach((element, index) => {
							if (settled(element)) {
								frame.push(place(`${selector} #${index}`, element));
							}
						});
				}
				frames.push(frame);
				requestAnimationFrame(sample);
			};
			requestAnimationFrame(sample);
		},
		{ textual: byText, ordered: byOrder },
	);
}

// landmarkKeys lists every landmark key recorded in any frame.
export async function landmarkKeys(page: Page) {
	const frames = await page.evaluate(
		() =>
			(window as unknown as { __landmarkFrames: Landmark[][] })
				.__landmarkFrames,
	);
	return new Set(frames.flat().map((landmark) => landmark.key));
}

// landmarkMoves lists every landmark whose position changed after it first
// appeared, with the positions it took in order.
export async function landmarkMoves(page: Page, tolerance = 1) {
	const frames = await page.evaluate(
		() =>
			(window as unknown as { __landmarkFrames: Landmark[][] })
				.__landmarkFrames,
	);
	const first = new Map<string, Landmark>();
	const moves = new Map<string, string[]>();
	for (const frame of frames) {
		for (const landmark of frame) {
			const origin = first.get(landmark.key);
			if (!origin) {
				first.set(landmark.key, landmark);
				continue;
			}
			if (
				Math.abs(origin.x - landmark.x) > tolerance ||
				Math.abs(origin.y - landmark.y) > tolerance
			) {
				const path = moves.get(landmark.key) ?? [`${origin.x},${origin.y}`];
				const step = `${landmark.x},${landmark.y}`;
				if (path.at(-1) !== step) path.push(step);
				moves.set(landmark.key, path);
			}
		}
	}
	return [...moves].map(([key, path]) => `${key}: ${path.join(" → ")}`);
}

// forgetLandmarks drops what was recorded so far, so a spec can watch one
// navigation without comparing it with the page it came from.
export async function forgetLandmarks(page: Page) {
	await page.evaluate(() => {
		(
			window as unknown as { __landmarkFrames: Landmark[][] }
		).__landmarkFrames.length = 0;
	});
}
