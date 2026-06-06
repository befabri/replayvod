// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { createElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RecordingWebhookDeliveryResponse as Delivery } from "@/api/generated/trpc";
import { RecordingWebhookDeliveries } from "./RecordingWebhookDeliveries";

const retryMock = vi.hoisted(() => ({ mutate: vi.fn() }));
const deliveriesMock = vi.hoisted(() => ({
	data: [] as Delivery[],
	isLoading: false,
}));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({
		t: (key: string, vars?: { count?: number }) =>
			vars?.count == null ? key : `${key}:${vars.count}`,
		i18n: { language: "en" },
	}),
}));

vi.mock("sonner", () => ({
	toast: {
		success: vi.fn(),
		error: vi.fn(),
	},
}));

vi.mock("@/components/ui/timestamp", () => ({
	Timestamp: ({ iso }: { iso: string }) => createElement("span", null, iso),
	TimestampValue: ({ iso }: { iso: string }) =>
		createElement("span", null, iso),
}));

vi.mock("../queries", () => ({
	useRecordingWebhookDeliveries: () => deliveriesMock,
	useRetryRecordingWebhookDelivery: () => ({
		mutate: retryMock.mutate,
		isPending: false,
		variables: undefined,
	}),
}));

afterEach(() => {
	cleanup();
	retryMock.mutate.mockClear();
	deliveriesMock.data = [];
	deliveriesMock.isLoading = false;
});

describe("RecordingWebhookDeliveries", () => {
	it("renders durable status and attempt count", () => {
		deliveriesMock.data = [
			delivery({ outcome: "pending", attempts: 2, status: 503 }),
		];

		render(createElement(RecordingWebhookDeliveries));

		expect(screen.getByText("webhook.outcome_pending")).toBeTruthy();
		expect(screen.getByText("webhook.delivery_attempts:2")).toBeTruthy();
		expect(screen.getByText("HTTP 503")).toBeTruthy();
	});

	it("queues retry for failed deliveries", () => {
		deliveriesMock.data = [delivery({ id: 9, outcome: "failed" })];

		render(createElement(RecordingWebhookDeliveries));
		fireEvent.click(
			screen.getByRole("button", { name: "webhook.retry_delivery" }),
		);

		expect(retryMock.mutate).toHaveBeenCalledWith(
			{ id: 9 },
			expect.objectContaining({
				onSuccess: expect.any(Function),
				onError: expect.any(Function),
			}),
		);
	});
});

function delivery(overrides: Partial<Delivery> = {}): Delivery {
	return {
		id: 1,
		time: "2026-05-30T12:00:00Z",
		event: "recording.completed",
		video_id: 42,
		outcome: "delivered",
		status: 200,
		attempts: 1,
		error: "",
		test: false,
		message_id: "msg",
		...overrides,
	};
}
