import type { A11yParameters } from "@storybook/addon-a11y";

type AxeCheck = NonNullable<
	NonNullable<A11yParameters["config"]>["checks"]
>[number];

type AcceptedPairs = {
	foreground: string;
	backgrounds: readonly string[];
};

export const ACCEPTED_PALETTE_CONTRAST = {
	lightPrimaryTextOnSurfaces: {
		foreground: "#7d8df8",
		backgrounds: ["#d8defc", "#eef0fc", "#f2f3f8", "#f2f4fe", "#fbfbfd"],
	},
	lightWhiteOnPrimary: {
		foreground: "#ffffff",
		backgrounds: ["#7d8df8"],
	},
	lightLinkOnSurfaces: {
		foreground: "#2e79f5",
		backgrounds: ["#f5f6fa", "#fbfbfd", "#ffffff"],
	},
	lightDestructiveOnTints: {
		foreground: "#df202e",
		backgrounds: ["#f0e4e9", "#f7dade", "#f8e5e8", "#fadee0"],
	},
	lightDestructiveHoverOnTint: {
		foreground: "#e13441",
		backgrounds: ["#f8e5e8"],
	},
	lightMutedTextOnSurface: {
		foreground: "#818489",
		backgrounds: ["#fbfbfd"],
	},
	darkPrimaryOnPrimaryTint: {
		foreground: "#8390fa",
		backgrounds: ["#3b3a6e"],
	},
	darkDestructiveOnSurfaces: {
		foreground: "#ef4444",
		backgrounds: ["#1c1a31", "#30151f", "#442944"],
	},
	darkDestructiveHoverOnTint: {
		foreground: "#db3f40",
		backgrounds: ["#25121d"],
	},
} satisfies Record<string, AcceptedPairs>;

function pairKey(foreground: string, background: string): string {
	return `${foreground.toLowerCase()} on ${background.toLowerCase()}`;
}

const ACCEPTED_PAIRS = new Set(
	Object.values(ACCEPTED_PALETTE_CONTRAST).flatMap(
		({ foreground, backgrounds }) =>
			backgrounds.map((background) => pairKey(foreground, background)),
	),
);

function isAcceptedPair(data: unknown): boolean {
	if (typeof data !== "object" || data === null) return false;
	const { fgColor, bgColor } = data as { fgColor?: unknown; bgColor?: unknown };
	return (
		typeof fgColor === "string" &&
		typeof bgColor === "string" &&
		ACCEPTED_PAIRS.has(pairKey(fgColor, bgColor))
	);
}

export const acceptedPaletteContrast: AxeCheck = {
	id: "color-contrast",
	after: (results) =>
		results.filter(
			(result) => result.result !== false || !isAcceptedPair(result.data),
		),
};
