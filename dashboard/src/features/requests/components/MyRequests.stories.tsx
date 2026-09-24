import { expect, fn, waitFor } from "storybook/test";
import preview from "#.storybook/preview";
import type { RequestIDInput } from "@/api/generated/trpc";
import { useMyScheduleRequests } from "@/features/requests";
import i18n from "@/i18n";
import { makeScheduleRequests } from "@/test/fixtures";
import { neverResolves, trpcError, trpcParameters } from "@/test/trpc-mock";
import { MyRequests } from "./MyRequests";

const REQUESTS = makeScheduleRequests(6);

const cancelRequest = fn((_input: RequestIDInput) => ({ ok: true }));

const meta = preview.meta({
	title: "Features/Requests/MyRequests",
	render: function Render() {
		return <MyRequests requests={useMyScheduleRequests()} />;
	},
	parameters: {
		layout: "padded",
		...trpcParameters({
			schedule: {
				myRequests: () => ({ items: REQUESTS }),
				cancelRequest,
			},
		}),
	},
});

export const Default = meta.story();

export const Empty = meta.story({
	parameters: trpcParameters({
		schedule: { myRequests: () => ({ items: [] }) },
	}),
	play: async ({ canvas }) => {
		await waitFor(() =>
			expect(canvas.getByText(i18n.t("requests.empty"))).toBeVisible(),
		);
	},
});

export const Loading = meta.story({
	parameters: trpcParameters({
		schedule: { myRequests: neverResolves },
	}),
});

export const LoadError = meta.story({
	parameters: trpcParameters({
		schedule: {
			myRequests: () => {
				throw trpcError("INTERNAL_SERVER_ERROR", 500, "database is locked");
			},
		},
	}),
	play: async ({ canvas }) => {
		await waitFor(() =>
			expect(
				canvas.getByText(new RegExp(i18n.t("requests.failed_to_load"))),
			).toBeVisible(),
		);
	},
});

export const HasMorePages = meta.story({
	parameters: trpcParameters({
		schedule: {
			myRequests: () => ({
				items: REQUESTS,
				next_cursor: { created_at: REQUESTS[5].created_at, id: REQUESTS[5].id },
			}),
		},
	}),
	play: async ({ canvas }) => {
		await waitFor(() =>
			expect(
				canvas.getByRole("button", { name: i18n.t("common.show_more") }),
			).toBeVisible(),
		);
	},
});

export const CancelPending = meta.story({
	play: async ({ canvas, userEvent }) => {
		const [cancel] = await canvas.findAllByRole("button", {
			name: i18n.t("requests.cancel"),
		});
		await userEvent.click(cancel);
		await waitFor(() =>
			expect(cancelRequest).toHaveBeenCalledWith({ id: REQUESTS[0].id }),
		);
	},
});
