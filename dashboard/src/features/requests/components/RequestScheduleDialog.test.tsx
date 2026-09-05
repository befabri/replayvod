// @vitest-environment jsdom

import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { createElement } from "react";
import { toast } from "sonner";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("sonner", () => ({
	toast: { success: vi.fn(), error: vi.fn() },
}));
const mutateAsync = vi.fn(async () => ({}));
vi.mock("@/features/requests", () => ({
	useCreateScheduleRequest: () => ({ mutateAsync, isPending: false }),
}));
// The real picker drags in the channel search query stack; a bare input
// forwarding onChange is enough to drive the form value.
vi.mock("@/features/channels/components/ChannelPicker", () => ({
	ChannelPicker: ({
		value,
		onChange,
		placeholder,
	}: {
		value: string;
		onChange: (id: string) => void;
		placeholder?: string;
	}) =>
		createElement("input", {
			value,
			placeholder,
			onChange: (e: React.ChangeEvent<HTMLInputElement>) =>
				onChange(e.target.value),
		}),
}));

import { RequestScheduleDialog } from "./RequestScheduleDialog";

afterEach(() => {
	cleanup();
	mutateAsync.mockClear();
	vi.clearAllMocks();
});

async function openAndFill(note: string) {
	render(createElement(RequestScheduleDialog));
	fireEvent.click(
		screen.getByRole("button", { name: "requests.request_title" }),
	);

	const channel = await screen.findByPlaceholderText(
		"requests.channel_placeholder",
	);
	fireEvent.change(channel, { target: { value: "b-1" } });
	fireEvent.change(screen.getByPlaceholderText("requests.note_placeholder"), {
		target: { value: note },
	});
	fireEvent.click(screen.getByRole("button", { name: "requests.create" }));
}

describe("RequestScheduleDialog", () => {
	it("submits the picked channel with the note trimmed", async () => {
		await openAndFill("  please record  ");
		await waitFor(() => {
			expect(mutateAsync).toHaveBeenCalledWith({
				broadcaster_id: "b-1",
				note: "please record",
			});
		});
	});

	it("drops a whitespace-only note instead of sending it", async () => {
		await openAndFill("   ");
		await waitFor(() => {
			expect(mutateAsync).toHaveBeenCalledWith({
				broadcaster_id: "b-1",
				note: undefined,
			});
		});
	});

	it("requires a channel before submitting", async () => {
		render(createElement(RequestScheduleDialog));
		fireEvent.click(
			screen.getByRole("button", { name: "requests.request_title" }),
		);
		const submit = await screen.findByRole<HTMLButtonElement>("button", {
			name: "requests.create",
		});
		expect(submit.disabled).toBe(true);
		fireEvent.click(submit);
		expect(mutateAsync).not.toHaveBeenCalled();
	});

	it("keeps the channel and note after failure and resets them after successful retry", async () => {
		mutateAsync.mockRejectedValueOnce(new Error("channel already scheduled"));
		await openAndFill("please record");
		await waitFor(() =>
			expect(toast.error).toHaveBeenCalledWith("channel already scheduled"),
		);
		expect(
			screen.getByPlaceholderText<HTMLInputElement>(
				"requests.channel_placeholder",
			).value,
		).toBe("b-1");
		expect(
			screen.getByPlaceholderText<HTMLInputElement>("requests.note_placeholder")
				.value,
		).toBe("please record");
		fireEvent.click(screen.getByRole("button", { name: "requests.create" }));
		await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
		expect(mutateAsync).toHaveBeenCalledTimes(2);
		fireEvent.click(
			screen.getByRole("button", { name: "requests.request_title" }),
		);
		expect(
			(
				await screen.findByPlaceholderText<HTMLInputElement>(
					"requests.channel_placeholder",
				)
			).value,
		).toBe("");
		expect(
			screen.getByPlaceholderText<HTMLInputElement>("requests.note_placeholder")
				.value,
		).toBe("");
	});
});
