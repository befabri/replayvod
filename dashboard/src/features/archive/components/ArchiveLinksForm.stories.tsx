import { expect, fn, waitFor } from "storybook/test";
import preview from "#.storybook/preview";
import type { EnqueueArchiveInput } from "@/api/generated/trpc";
import i18n from "@/i18n";
import { makeEnqueueResults, vodLinks } from "@/test/fixtures";
import { neverResolves, trpcParameters } from "@/test/trpc-mock";
import { MAX_VODS_PER_ENQUEUE } from "../limits";
import { DEFAULT_ARCHIVE_SETTINGS } from "../settings";
import { ArchiveLinksForm } from "./ArchiveLinksForm";

const LINKS = vodLinks(3);

const enqueue = fn(({ vods }: EnqueueArchiveInput) => ({
	items: makeEnqueueResults(vods),
}));

function linksField(canvas: { getByLabelText: (text: string) => HTMLElement }) {
	return canvas.getByLabelText(i18n.t("archive.links_label"));
}

const meta = preview.meta({
	title: "Features/Archive/ArchiveLinksForm",
	component: ArchiveLinksForm,
	args: { settings: DEFAULT_ARCHIVE_SETTINGS },
	argTypes: { settings: { control: false } },
	parameters: {
		layout: "padded",
		...trpcParameters({ archive: { enqueue } }),
	},
	decorators: [
		(Story) => (
			<div className="max-w-2xl">
				<Story />
			</div>
		),
	],
});

export const Empty = meta.story({
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("button", { name: i18n.t("archive.submit") }),
		).toBeDisabled();
		await expect(canvas.getByText(i18n.t("archive.links_hint"))).toBeVisible();
	},
});

export const ValidLinks = meta.story({
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(linksField(canvas));
		await userEvent.paste(LINKS.join("\n"));
		await expect(
			canvas.getByRole("button", {
				name: i18n.t("archive.submit_count", { count: LINKS.length }),
			}),
		).toBeEnabled();
	},
});

export const InvalidEntries = meta.story({
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(linksField(canvas));
		await userEvent.paste([LINKS[0], "not a link", "also wrong"].join("\n"));
		await expect(
			canvas.getByText(i18n.t("archive.links_invalid", { count: 2 })),
		).toBeVisible();
		await expect(
			canvas.getByRole("button", {
				name: i18n.t("archive.submit_count", { count: 1 }),
			}),
		).toBeEnabled();
	},
});

export const TooManyLinks = meta.story({
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(linksField(canvas));
		await userEvent.paste(vodLinks(MAX_VODS_PER_ENQUEUE + 1).join("\n"));
		await expect(
			canvas.getByText(
				i18n.t("archive.links_too_many", { max: MAX_VODS_PER_ENQUEUE }),
			),
		).toBeVisible();
		await expect(
			canvas.getByRole("button", {
				name: i18n.t("archive.submit_count", {
					count: MAX_VODS_PER_ENQUEUE + 1,
				}),
			}),
		).toBeDisabled();
	},
});

export const Submitting = meta.story({
	parameters: trpcParameters({ archive: { enqueue: neverResolves } }),
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(linksField(canvas));
		await userEvent.paste(LINKS.join("\n"));
		await userEvent.click(
			canvas.getByRole("button", {
				name: i18n.t("archive.submit_count", { count: LINKS.length }),
			}),
		);
		await waitFor(() =>
			expect(
				canvas.getByRole("button", { name: i18n.t("common.saving") }),
			).toBeDisabled(),
		);
	},
});

export const Submitted = meta.story({
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(linksField(canvas));
		await userEvent.paste([...LINKS, "not a link"].join("\n"));
		await userEvent.click(
			canvas.getByRole("button", {
				name: i18n.t("archive.submit_count", { count: LINKS.length }),
			}),
		);
		await waitFor(() =>
			expect(enqueue).toHaveBeenCalledWith(
				expect.objectContaining({ vods: LINKS }),
			),
		);
		await waitFor(() => expect(linksField(canvas)).toHaveValue(""));
	},
});
