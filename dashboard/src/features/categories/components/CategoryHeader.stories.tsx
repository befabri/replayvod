import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { makeCategoryDetail } from "@/test/fixtures";
import { CategoryHeader, CategoryHeaderSkeleton } from "./CategoryHeader";

const CATEGORY = makeCategoryDetail(2);

const meta = preview.meta({
	title: "Features/Categories/CategoryHeader",
	component: CategoryHeader,
	args: { category: CATEGORY },
	argTypes: { category: { control: false } },
	parameters: { layout: "padded" },
});

export const Default = meta.story({
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("img", { name: CATEGORY.name }),
		).toBeVisible();
	},
});

// Firefox paints an image's alt text, in the image's text colour, until the
// image has loaded. The box art keeps its name for screen readers but must
// never flash it as text over the empty frame.
export const NameNeverPaintedWhileLoading = meta.story({
	play: async ({ canvas }) => {
		const art = canvas.getByRole("img", { name: CATEGORY.name });
		await expect(getComputedStyle(art).color).toBe("rgba(0, 0, 0, 0)");
	},
});

export const WithoutBoxArt = meta.story({
	args: { category: makeCategoryDetail(3, { box_art_url: undefined }) },
});

export const WithoutDescription = meta.story({
	args: { category: makeCategoryDetail(4, { description: undefined }) },
});

export const Loading = meta.story({
	render: () => <CategoryHeaderSkeleton />,
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
	},
});
