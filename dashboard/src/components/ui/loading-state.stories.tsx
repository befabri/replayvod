import { useTranslation } from "react-i18next";
import { expect } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { LoadingState } from "./loading-state";
import { Skeleton } from "./skeleton";

const meta = preview.meta({
	title: "UI/LoadingState",
	component: LoadingState,
});

export const Default = meta.story({
	play: async ({ canvas }) => {
		await expect(canvas.getByRole("status")).toHaveTextContent(
			i18n.t("common.loading"),
		);
	},
});

export const CustomLabel = meta.story({
	render: function Render(args) {
		const { t } = useTranslation();
		return <LoadingState {...args} label={t("common.saving")} />;
	},
	play: async ({ canvas }) => {
		await expect(canvas.getByRole("status")).toHaveTextContent(
			i18n.t("common.saving"),
		);
	},
});

export const WithSkeleton = meta.story({
	args: {
		className: "grid w-[40rem] grid-cols-3 gap-4",
		children: [1, 2, 3].map((n) => (
			<Skeleton key={n} className="aspect-video w-full" />
		)),
	},
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
	},
});
