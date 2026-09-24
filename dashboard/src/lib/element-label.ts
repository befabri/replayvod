import { type RefObject, useCallback, useMemo, useRef, useState } from "react";

type Labelable = HTMLButtonElement | HTMLInputElement;

export type PopupLabel<T extends Labelable> = {
	label: string | undefined;
	anchorRef: RefObject<T | null>;
	popupRef: (popup: HTMLElement | null) => void;
};

function textOf(elements: Iterable<Element | null | undefined>) {
	return [...elements]
		.map((element) => element?.textContent?.trim())
		.filter(Boolean)
		.join(" ");
}

export function elementLabel(element: Labelable): string | undefined {
	const labelledBy = textOf(
		(element.getAttribute("aria-labelledby") ?? "")
			.split(/\s+/)
			.filter(Boolean)
			.map((id) => element.ownerDocument.getElementById(id)),
	);
	return (
		labelledBy ||
		element.getAttribute("aria-label") ||
		textOf(element.labels ?? []) ||
		element.getAttribute("placeholder") ||
		undefined
	);
}

export function usePopupLabel<T extends Labelable>(): PopupLabel<T> {
	const anchorRef = useRef<T>(null);
	const [label, setLabel] = useState<string>();
	const popupRef = useCallback((popup: HTMLElement | null) => {
		if (popup && anchorRef.current) setLabel(elementLabel(anchorRef.current));
	}, []);
	return useMemo(() => ({ label, anchorRef, popupRef }), [label, popupRef]);
}
