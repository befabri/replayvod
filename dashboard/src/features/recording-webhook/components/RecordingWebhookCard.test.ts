// @vitest-environment jsdom

import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import { createElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RecordingWebhookConfigResponse as ConfigResponse } from "@/api/generated/trpc";
import {
	buildPayload,
	formStateFromConfig,
	RecordingWebhookCard,
} from "./RecordingWebhookCard";

const updateMock = vi.hoisted(() => ({
	mutate: vi.fn(),
	mutateAsync: vi.fn().mockResolvedValue(undefined),
}));
const regenerateMock = vi.hoisted(() => ({ mutate: vi.fn() }));
const testMock = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("../queries", () => ({
	useUpdateRecordingWebhookConfig: () => ({
		mutate: updateMock.mutate,
		mutateAsync: updateMock.mutateAsync,
		isPending: false,
		isError: false,
		isSuccess: false,
		error: undefined,
	}),
	useRegenerateRecordingWebhookSecret: () => ({
		mutate: regenerateMock.mutate,
		isPending: false,
	}),
	useTestRecordingWebhookDelivery: () => ({
		mutate: testMock.mutate,
		isPending: false,
		isSuccess: false,
		data: undefined,
	}),
}));

afterEach(() => {
	cleanup();
	updateMock.mutate.mockClear();
	updateMock.mutateAsync.mockClear();
	regenerateMock.mutate.mockClear();
	testMock.mutate.mockClear();
});

function configResponse(
	overrides: Partial<ConfigResponse> = {},
): ConfigResponse {
	return {
		enabled: true,
		url: "https://hooks.example.com/replayvod",
		secret: "abc123",
		events: [],
		...overrides,
	};
}

describe("formStateFromConfig", () => {
	it("maps an empty events list to both boxes checked (all events)", () => {
		const s = formStateFromConfig(configResponse({ events: [] }));
		expect(s.onCompleted).toBe(true);
		expect(s.onFailed).toBe(true);
	});

	it("maps a single event to only that box checked", () => {
		const s = formStateFromConfig(
			configResponse({ events: ["recording.failed"] }),
		);
		expect(s.onCompleted).toBe(false);
		expect(s.onFailed).toBe(true);
	});
});

describe("buildPayload", () => {
	it("sends only checked events, trims the URL, and never sends a secret", () => {
		const p = buildPayload({
			enabled: true,
			url: "  https://hooks.example.com/x  ",
			onCompleted: true,
			onFailed: false,
		});
		expect(p).toEqual({
			enabled: true,
			url: "https://hooks.example.com/x",
			events: ["recording.completed"],
		});
		expect("regenerate_secret" in p).toBe(false);
		expect("secret" in p).toBe(false);
	});
});

describe("RecordingWebhookCard", () => {
	it("submits the edited form through the mutation payload", async () => {
		render(createElement(RecordingWebhookCard, { data: configResponse() }));

		fireEvent.change(screen.getByLabelText("webhook.url"), {
			target: { value: "https://new.example/hook" },
		});
		fireEvent.click(screen.getByRole("button", { name: "webhook.save" }));

		await waitFor(() => {
			expect(updateMock.mutateAsync).toHaveBeenCalledWith({
				enabled: true,
				url: "https://new.example/hook",
				events: ["recording.completed", "recording.failed"],
			});
		});
	});

	it("blocks saving when enabled with no events selected", () => {
		render(createElement(RecordingWebhookCard, { data: configResponse() }));

		// Both event boxes start checked (empty events = all); uncheck both.
		for (const box of screen.getAllByRole("checkbox")) {
			fireEvent.click(box);
		}
		// The guard message appears and the disabled submit never fires the save.
		expect(screen.getByText("webhook.events_required")).toBeTruthy();
		fireEvent.click(screen.getByRole("button", { name: "webhook.save" }));
		expect(updateMock.mutateAsync).not.toHaveBeenCalled();
	});

	it("regenerate is a deliberate, confirmed action (not a form save)", () => {
		render(createElement(RecordingWebhookCard, { data: configResponse() }));

		// Clicking the button opens a confirmation, it does not rotate yet.
		fireEvent.click(
			screen.getByRole("button", { name: "webhook.regenerate_secret" }),
		);
		expect(regenerateMock.mutate).not.toHaveBeenCalled();
		expect(updateMock.mutateAsync).not.toHaveBeenCalled();

		// Confirming inside the dialog rotates without sending the config form.
		const dialog = screen.getByRole("dialog");
		fireEvent.click(
			within(dialog).getByRole("button", { name: "webhook.regenerate_secret" }),
		);
		expect(regenerateMock.mutate).toHaveBeenCalledTimes(1);
		expect(updateMock.mutateAsync).not.toHaveBeenCalled();
	});

	it("send test fires the test mutation", () => {
		render(createElement(RecordingWebhookCard, { data: configResponse() }));
		fireEvent.click(screen.getByRole("button", { name: "webhook.send_test" }));
		expect(testMock.mutate).toHaveBeenCalledTimes(1);
	});

	it("disables send test while the form has unsaved changes", () => {
		render(createElement(RecordingWebhookCard, { data: configResponse() }));

		fireEvent.change(screen.getByLabelText("webhook.url"), {
			target: { value: "https://new.example/hook" },
		});

		const sendTest = screen.getByRole("button", {
			name: "webhook.send_test",
		}) as HTMLButtonElement;
		expect(sendTest.disabled).toBe(true);
		fireEvent.click(sendTest);
		expect(testMock.mutate).not.toHaveBeenCalled();
	});

	it("keeps unsaved edits across a background config refetch", () => {
		const { rerender } = render(
			createElement(RecordingWebhookCard, { data: configResponse() }),
		);

		const url = screen.getByLabelText("webhook.url") as HTMLInputElement;
		fireEvent.change(url, {
			target: { value: "https://new.example/hook" },
		});

		rerender(
			createElement(RecordingWebhookCard, {
				data: configResponse(),
			}),
		);

		expect(url.value).toBe("https://new.example/hook");
	});

	it("re-enables send test once a successful save clears the dirty state", async () => {
		render(createElement(RecordingWebhookCard, { data: configResponse() }));

		fireEvent.change(screen.getByLabelText("webhook.url"), {
			target: { value: "https://new.example/hook" },
		});
		expect(
			(
				screen.getByRole("button", {
					name: "webhook.send_test",
				}) as HTMLButtonElement
			).disabled,
		).toBe(true);

		fireEvent.click(screen.getByRole("button", { name: "webhook.save" }));

		await waitFor(() => {
			expect(
				(
					screen.getByRole("button", {
						name: "webhook.send_test",
					}) as HTMLButtonElement
				).disabled,
			).toBe(false);
		});
	});

	it("keeps the form dirty for a retry when the save fails", async () => {
		updateMock.mutateAsync.mockRejectedValueOnce(new Error("save failed"));
		render(createElement(RecordingWebhookCard, { data: configResponse() }));

		fireEvent.change(screen.getByLabelText("webhook.url"), {
			target: { value: "https://new.example/hook" },
		});
		fireEvent.click(screen.getByRole("button", { name: "webhook.save" }));

		await waitFor(() => {
			expect(updateMock.mutateAsync).toHaveBeenCalled();
		});
		// The rejection is swallowed (no reset), so the form stays dirty and
		// "Send test" remains disabled. A leaked rejection would fail this test.
		expect(
			(
				screen.getByRole("button", {
					name: "webhook.send_test",
				}) as HTMLButtonElement
			).disabled,
		).toBe(true);
	});

	it("shows the signing secret masked and read-only when present", () => {
		render(createElement(RecordingWebhookCard, { data: configResponse() }));
		const secret = screen.getByLabelText("webhook.secret") as HTMLInputElement;
		expect(secret.value).toBe("abc123");
		expect(secret.readOnly).toBe(true);
		expect(secret.type).toBe("password");
	});

	it("hides the secret field and regenerate button when no secret exists yet", () => {
		render(
			createElement(RecordingWebhookCard, {
				data: configResponse({ secret: "" }),
			}),
		);
		expect(screen.queryByLabelText("webhook.secret")).toBeNull();
		expect(
			screen.queryByRole("button", { name: "webhook.regenerate_secret" }),
		).toBeNull();
	});
});
