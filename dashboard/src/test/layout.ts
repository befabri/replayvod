type Anchor = { element: Element; text: string };

function ownText(element: Element) {
	return [...element.childNodes]
		.filter((node) => node.nodeType === Node.TEXT_NODE)
		.map((node) => node.textContent ?? "")
		.join("")
		.replace(/\s+/g, " ")
		.trim();
}

function visuallyHidden(element: Element, root: Element) {
	for (
		let node: Element | null = element;
		node && node !== root;
		node = node.parentElement
	) {
		const box = node.getBoundingClientRect();
		if (box.width <= 1 || box.height <= 1) return true;
	}
	return false;
}

function anchorsIn(root: Element): Anchor[] {
	return [...root.querySelectorAll("*")]
		.filter(
			(element) =>
				!element.closest('[data-slot="skeleton"]') &&
				!visuallyHidden(element, root),
		)
		.map((element) => ({ element, text: ownText(element) }))
		.filter((anchor) => anchor.text.length > 0);
}

function offset(element: Element, root: Element) {
	const box = element.getBoundingClientRect();
	const origin = root.getBoundingClientRect();
	return { top: box.top - origin.top, left: box.left - origin.left };
}

export type LayoutCheck = {
	tolerance?: number;
	axis?: "both" | "vertical";
};

export function layoutMismatches(
	skeleton: Element,
	loaded: Element,
	{ tolerance = 1, axis = "both" }: LayoutCheck = {},
): string[] {
	const mismatches: string[] = [];
	const skeletonHeight = skeleton.getBoundingClientRect().height;
	const loadedHeight = loaded.getBoundingClientRect().height;
	if (Math.abs(skeletonHeight - loadedHeight) > tolerance) {
		mismatches.push(
			`height: skeleton ${Math.round(skeletonHeight)}px, loaded ${Math.round(loadedHeight)}px`,
		);
	}
	const candidates = anchorsIn(loaded);
	const anchors = anchorsIn(skeleton);
	if (anchors.length === 0) {
		mismatches.push("the skeleton shows none of the loaded text");
	}
	for (const anchor of anchors) {
		const index = candidates.findIndex(
			(candidate) =>
				candidate.text === anchor.text &&
				candidate.element.tagName === anchor.element.tagName,
		);
		if (index === -1) {
			mismatches.push(`"${anchor.text}": not in the loaded layout`);
			continue;
		}
		const [match] = candidates.splice(index, 1);
		const expected = offset(match.element, loaded);
		const actual = offset(anchor.element, skeleton);
		if (
			Math.abs(expected.top - actual.top) > tolerance ||
			(axis === "both" && Math.abs(expected.left - actual.left) > tolerance)
		) {
			mismatches.push(
				`"${anchor.text}": skeleton at ${Math.round(actual.left)},${Math.round(actual.top)}, loaded at ${Math.round(expected.left)},${Math.round(expected.top)}`,
			);
		}
	}
	return mismatches;
}
