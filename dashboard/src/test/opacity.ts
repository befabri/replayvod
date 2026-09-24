export function effectiveOpacity(element: Element): number {
	let opacity = 1;
	for (
		let node: Element | null = element;
		node !== null;
		node = node.parentElement
	) {
		opacity *= Number(getComputedStyle(node).opacity);
	}
	return opacity;
}
