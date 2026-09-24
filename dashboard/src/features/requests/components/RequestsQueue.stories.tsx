import { expect, fn, screen, waitFor } from "storybook/test";
import preview from "#.storybook/preview";
import type {
	RequestIDInput,
	ScheduleRequestResponse,
} from "@/api/generated/trpc";
import { useAllScheduleRequests } from "@/features/requests";
import i18n from "@/i18n";
import {
	makeScheduleRequest,
	makeScheduleRequests,
	TAGS,
} from "@/test/fixtures";
import { neverResolves, trpcError, trpcParameters } from "@/test/trpc-mock";
import { RequestsQueue } from "./RequestsQueue";

const REQUESTS = makeScheduleRequests(6);

const rejectRequest = fn((_input: RequestIDInput) => ({ ok: true }));

const meta = preview.meta({
	title: "Features/Requests/RequestsQueue",
	render: function Render() {
		return <RequestsQueue requests={useAllScheduleRequests()} />;
	},
	parameters: {
		layout: "padded",
		...trpcParameters({
			schedule: {
				requests: () => ({ items: REQUESTS }),
				rejectRequest,
			},
			tag: { list: () => [...TAGS] },
		}),
	},
});

export const Default = meta.story();

// The section keeps its place while the queue loads, so the schedules below
// do not jump down when the requests arrive.
export const Loading = meta.story({
	parameters: trpcParameters({ schedule: { requests: neverResolves } }),
	play: async ({ canvas }) => {
		await expect(
			canvas.getByRole("heading", { name: i18n.t("requests.queue_title") }),
		).toBeVisible();
		await expect(canvas.getByRole("status")).toHaveTextContent(
			i18n.t("common.loading"),
		);
		await expect(canvas.getByRole("table")).toHaveAttribute(
			"aria-busy",
			"true",
		);
	},
});

const noRequests = fn(() => ({ items: [] as ScheduleRequestResponse[] }));

export const Empty = meta.story({
	parameters: trpcParameters({ schedule: { requests: noRequests } }),
	play: async ({ canvas }) => {
		await waitFor(() => expect(noRequests).toHaveBeenCalled());
		await waitFor(() =>
			expect(
				canvas.queryByRole("heading", {
					name: i18n.t("requests.queue_title"),
				}),
			).not.toBeInTheDocument(),
		);
	},
});

let lastPending: ScheduleRequestResponse[] = [];

export const HiddenOnceEmpty = meta.story({
	beforeEach: () => {
		lastPending = [makeScheduleRequest(0, { status: "PENDING" })];
	},
	parameters: trpcParameters({
		schedule: {
			requests: () => ({ items: lastPending }),
			rejectRequest: ({ id }) => {
				lastPending = lastPending.filter((request) => request.id !== id);
				return { ok: true };
			},
		},
	}),
	play: async ({ canvas, userEvent }) => {
		const title = await canvas.findByText(i18n.t("requests.queue_title"));
		await userEvent.click(
			canvas.getByRole("button", { name: i18n.t("requests.reject") }),
		);
		await waitFor(() => expect(title).not.toBeInTheDocument());
	},
});

export const LoadError = meta.story({
	parameters: trpcParameters({
		schedule: {
			requests: () => {
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

export const OpenApproveDialog = meta.story({
	play: async ({ canvas, userEvent }) => {
		const [approve] = await canvas.findAllByRole("button", {
			name: i18n.t("requests.approve"),
		});
		await userEvent.click(approve);
		await waitFor(() => expect(screen.getByRole("dialog")).toBeVisible());
		await expect(
			screen.getByText(i18n.t("requests.approve_title")),
		).toBeVisible();
	},
});

export const Reject = meta.story({
	play: async ({ canvas, userEvent }) => {
		const [reject] = await canvas.findAllByRole("button", {
			name: i18n.t("requests.reject"),
		});
		await userEvent.click(reject);
		await waitFor(() =>
			expect(rejectRequest).toHaveBeenCalledWith({ id: REQUESTS[0].id }),
		);
	},
});
