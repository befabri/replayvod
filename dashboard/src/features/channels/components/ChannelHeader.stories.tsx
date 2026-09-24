import { expect, fn, waitFor } from "storybook/test";
import preview from "#.storybook/preview";
import type { SetFavoriteInput } from "@/api/generated/trpc";
import i18n from "@/i18n";
import { FIXTURE_NOW, makeChannelResponse } from "@/test/fixtures";
import { trpcParameters } from "@/test/trpc-mock";
import { ChannelHeader, ChannelHeaderSkeleton } from "./ChannelHeader";

const CHANNEL = makeChannelResponse(0);

const setFavorite = fn(({ favorite }: SetFavoriteInput) => ({
	favorite,
	updated_at: new Date(FIXTURE_NOW).toISOString(),
}));

const meta = preview.meta({
	title: "Features/Channels/ChannelHeader",
	component: ChannelHeader,
	args: { channel: CHANNEL, isLive: false, canDownload: false },
	argTypes: { channel: { control: false } },
	parameters: {
		layout: "padded",
		...trpcParameters({ channel: { setFavorite } }),
	},
});

export const Default = meta.story({
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("button", { name: i18n.t("channels.favorite.label") }),
		).toHaveAttribute("aria-pressed", "false");
		await expect(
			canvas.getByRole("link", { name: i18n.t("channels.open_in_twitch") }),
		).toHaveAttribute("href", `https://twitch.tv/${CHANNEL.broadcaster_login}`);
		await expect(
			canvas.queryByRole("button", { name: i18n.t("videos.trigger_download") }),
		).not.toBeInTheDocument();
	},
});

export const Favorited = meta.story({
	args: {
		channel: makeChannelResponse(0, {
			user_state: {
				favorite: true,
				updated_at: new Date(FIXTURE_NOW).toISOString(),
			},
		}),
	},
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("button", { name: i18n.t("channels.favorite.label") }),
		).toHaveAttribute("aria-pressed", "true");
	},
});

export const CanDownload = meta.story({
	args: { canDownload: true },
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("button", { name: i18n.t("videos.trigger_download") }),
		).toBeVisible();
	},
});

export const Live = meta.story({
	args: { isLive: true, canDownload: true },
});

export const WithoutDescription = meta.story({
	args: { channel: makeChannelResponse(1, { description: undefined }) },
});

export const ToggleFavorite = meta.story({
	play: async ({ canvas, userEvent }) => {
		await userEvent.click(
			canvas.getByRole("button", { name: i18n.t("channels.favorite.label") }),
		);
		await waitFor(() =>
			expect(setFavorite).toHaveBeenCalledWith({
				broadcaster_id: CHANNEL.broadcaster_id,
				favorite: true,
			}),
		);
		await expect(
			canvas.getByRole("button", { name: i18n.t("channels.favorite.label") }),
		).toHaveAttribute("aria-pressed", "true");
	},
});

export const Loading = meta.story({
	render: ({ canDownload }) => (
		<ChannelHeaderSkeleton canDownload={canDownload} />
	),
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("status", { name: i18n.t("common.loading") }),
		).toBeVisible();
	},
});

export const LoadingCanDownload = Loading.extend({
	args: { canDownload: true },
});
