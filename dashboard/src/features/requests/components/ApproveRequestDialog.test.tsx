// @vitest-environment jsdom

import {
	act,
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { createElement } from "react";
import { toast } from "sonner";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ScheduleRequestResponse } from "@/features/requests";

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("sonner", () => ({
	toast: { success: vi.fn(), error: vi.fn() },
}));
const mutateAsync = vi.fn(async () => ({}));
vi.mock("@/features/requests", () => ({
	useApproveScheduleRequest: () => ({
		mutateAsync,
		isPending: false,
		isError: false,
		error: null,
	}),
}));
// Keep the form, validation, recording controls, and filter toggles real.
// Only the category search and tag data source are replaced with fixtures.
vi.mock("@/features/tags", () => ({
	useTags: () => ({ data: [{ id: 11, name: "English" }] }),
}));
vi.mock("@/features/categories/components/CategoryMultiPicker", () => ({
	CategoryMultiPicker: ({
		selected,
		onChange,
		disabled,
	}: {
		selected: string[];
		onChange: (ids: string[]) => void;
		disabled: boolean;
	}) =>
		createElement(
			"select",
			{
				"aria-label": "Categories",
				value: selected[0] ?? "",
				disabled,
				onChange: (e: React.ChangeEvent<HTMLSelectElement>) =>
					onChange(e.target.value ? [e.target.value] : []),
			},
			createElement("option", { value: "" }, "Choose"),
			createElement("option", { value: "game-1" }, "Game One"),
		),
}));

import { ApproveRequestDialog } from "./ApproveRequestDialog";

const request: ScheduleRequestResponse = {
	id: 7,
	broadcaster_id: "123456",
	broadcaster_login: "chanone",
	broadcaster_name: "Chan One",
	requested_by: "u-viewer",
	requested_by_name: "Vera Viewer",
	status: "PENDING",
	created_at: "2026-06-01T12:00:00Z",
};

afterEach(() => {
	cleanup();
	mutateAsync.mockClear();
	vi.clearAllMocks();
});

describe("ApproveRequestDialog", () => {
	it("submits request_id plus the schedule payload with is_disabled false", async () => {
		const onClose = vi.fn();
		render(createElement(ApproveRequestDialog, { request, onClose }));

		fireEvent.click(screen.getByRole("button", { name: "requests.approve" }));

		await waitFor(() => {
			expect(mutateAsync).toHaveBeenCalledTimes(1);
		});
		expect(mutateAsync).toHaveBeenCalledWith({
			request_id: 7,
			recording_type: "video",
			quality: "HIGH",
			force_h264: false,
			has_min_viewers: false,
			min_viewers: undefined,
			has_categories: false,
			has_tags: false,
			is_delete_rediff: false,
			time_before_delete: undefined,
			category_ids: [],
			tag_ids: [],
			is_disabled: false,
		});
		await waitFor(() => {
			expect(onClose).toHaveBeenCalled();
		});
	});

	it("stays open and skips onClose when the approval fails", async () => {
		mutateAsync.mockRejectedValueOnce(new Error("boom"));
		const onClose = vi.fn();
		render(createElement(ApproveRequestDialog, { request, onClose }));

		fireEvent.click(screen.getByRole("button", { name: "requests.approve" }));

		await waitFor(() => {
			expect(toast.error).toHaveBeenCalledWith("boom");
		});
		expect(onClose).not.toHaveBeenCalled();
	});

	it("submits the admin's edited settings and selected filters", async () => {
		render(createElement(ApproveRequestDialog, { request, onClose: vi.fn() }));
		fireEvent.click(
			screen.getByRole("checkbox", { name: "schedules.force_h264" }),
		);
		fireEvent.click(
			screen.getByRole("combobox", { name: "schedules.quality" }),
		);
		const lowQuality = await screen.findByRole("option", {
			name: "schedules.quality_low",
		});
		fireEvent.pointerDown(lowQuality, { pointerType: "mouse", button: 0 });
		fireEvent.click(lowQuality);
		await waitFor(() =>
			expect(
				screen.getByRole("combobox", { name: "schedules.quality" }).textContent,
			).toContain("schedules.quality_low"),
		);
		fireEvent.click(
			screen.getByRole("checkbox", { name: "schedules.has_min_viewers" }),
		);
		fireEvent.change(screen.getByLabelText("schedules.min_viewers"), {
			target: { value: "42" },
		});
		fireEvent.click(
			screen.getByRole("checkbox", { name: "schedules.is_delete_rediff" }),
		);
		fireEvent.change(screen.getByLabelText("schedules.time_before_delete"), {
			target: { value: "24" },
		});
		fireEvent.click(
			screen.getByRole("checkbox", { name: "schedules.has_categories" }),
		);
		fireEvent.change(screen.getByLabelText("Categories"), {
			target: { value: "game-1" },
		});
		fireEvent.click(
			screen.getByRole("checkbox", { name: "schedules.has_tags" }),
		);
		fireEvent.click(screen.getByRole("checkbox", { name: "English" }));
		fireEvent.click(screen.getByRole("button", { name: "requests.approve" }));
		await waitFor(() =>
			expect(mutateAsync).toHaveBeenCalledWith({
				request_id: request.id,
				recording_type: "video",
				quality: "LOW",
				force_h264: true,
				has_min_viewers: true,
				min_viewers: 42,
				has_categories: true,
				category_ids: ["game-1"],
				has_tags: true,
				tag_ids: [11],
				is_delete_rediff: true,
				time_before_delete: 24,
				is_disabled: false,
			}),
		);
	});

	it("clears the H.264 override when switching an approval to audio", async () => {
		render(createElement(ApproveRequestDialog, { request, onClose: vi.fn() }));
		fireEvent.click(
			screen.getByRole("checkbox", { name: "schedules.force_h264" }),
		);
		fireEvent.click(
			screen.getByRole("radio", { name: "schedules.mode_audio" }),
		);
		expect(
			screen
				.getByRole("checkbox", { name: "schedules.force_h264" })
				.getAttribute("aria-checked"),
		).toBe("false");
		fireEvent.click(screen.getByRole("button", { name: "requests.approve" }));
		await waitFor(() =>
			expect(mutateAsync).toHaveBeenCalledWith(
				expect.objectContaining({
					request_id: request.id,
					recording_type: "audio",
					force_h264: false,
				}),
			),
		);
	});

	it("blocks an empty enabled filter and allows correction before submitting", async () => {
		render(createElement(ApproveRequestDialog, { request, onClose: vi.fn() }));
		fireEvent.click(
			screen.getByRole("checkbox", { name: "schedules.has_categories" }),
		);
		fireEvent.click(screen.getByRole("button", { name: "requests.approve" }));
		await screen.findByText(
			"category_ids must include at least one category when has_categories is enabled",
		);
		expect(mutateAsync).not.toHaveBeenCalled();
		fireEvent.change(screen.getByLabelText("Categories"), {
			target: { value: "game-1" },
		});
		fireEvent.click(screen.getByRole("button", { name: "requests.approve" }));
		await waitFor(() => expect(mutateAsync).toHaveBeenCalledTimes(1));
	});

	it("blocks duplicate submissions while approval is in flight", async () => {
		let resolveApproval: (value: object) => void = () => {};
		const pending = new Promise<object>((resolve) => {
			resolveApproval = resolve;
		});
		mutateAsync.mockReturnValueOnce(pending);
		const onClose = vi.fn();
		render(createElement(ApproveRequestDialog, { request, onClose }));
		fireEvent.click(screen.getByRole("button", { name: "requests.approve" }));
		await waitFor(() => expect(mutateAsync).toHaveBeenCalledTimes(1));
		const saving = screen.getByRole<HTMLButtonElement>("button", {
			name: "common.saving",
		});
		expect(saving.disabled).toBe(true);
		fireEvent.click(saving);
		expect(mutateAsync).toHaveBeenCalledTimes(1);
		expect(onClose).not.toHaveBeenCalled();
		await act(async () => resolveApproval({}));
		await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
	});
});
