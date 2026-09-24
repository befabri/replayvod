import { expect, fn } from "storybook/test";
import preview from "#.storybook/preview";
import i18n from "@/i18n";
import { useStoryArg } from "@/test/story-args";
import { Pager } from "./pager";

const meta = preview.meta({
	title: "UI/Pager",
	component: Pager,
	args: {
		page: 0,
		hasNext: true,
		total: 214,
		onPrev: fn(),
		onNext: fn(),
	},
	render: function Render(args, context) {
		const [page, setPage] = useStoryArg(args, "page", context);
		return (
			<Pager
				{...args}
				page={page}
				onPrev={() => {
					args.onPrev();
					setPage(page - 1);
				}}
				onNext={() => {
					args.onNext();
					setPage(page + 1);
				}}
			/>
		);
	},
});

export const FirstPage = meta.story({
	play: async ({ args, canvas, userEvent }) => {
		await expect(
			canvas.getByRole("button", { name: i18n.t("common.previous") }),
		).toBeDisabled();
		await userEvent.click(
			canvas.getByRole("button", { name: i18n.t("common.next") }),
		);
		await expect(args.onNext).toHaveBeenCalledOnce();
		await expect(
			canvas.getByText(i18n.t("common.page", { n: 2 }), { exact: false }),
		).toBeVisible();
	},
});

export const MiddlePage = meta.story({
	args: { page: 2 },
});

export const LastPage = meta.story({
	args: { page: 4, hasNext: false },
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("button", { name: i18n.t("common.next") }),
		).toBeDisabled();
		await expect(
			canvas.getByRole("button", { name: i18n.t("common.previous") }),
		).toBeEnabled();
	},
});

export const WithoutTotal = meta.story({
	args: { total: undefined, page: 1 },
});

export const SinglePage = meta.story({
	args: { total: 12, hasNext: false },
	play: async ({ canvas }) => {
		for (const name of [i18n.t("common.previous"), i18n.t("common.next")]) {
			await expect(canvas.getByRole("button", { name })).toBeDisabled();
		}
	},
});
