// @vitest-environment jsdom

import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { createElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { StorageState } from "@/api/generated/trpc";

const adopt = vi.hoisted(() => ({
	mutateAsync:
		vi.fn<
			() => Promise<{ scan_status: "scheduled" | "disabled" | "failed" }>
		>(),
	isPending: false,
}));
const toast = vi.hoisted(() => ({
	success: vi.fn(),
	error: vi.fn(),
	warning: vi.fn(),
}));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({
		t: (key: string, vars?: Record<string, unknown>) =>
			vars?.location ? `${key}:${vars.location}` : key,
		i18n: { language: "en" },
	}),
}));
vi.mock("sonner", () => ({ toast }));
vi.mock("@/features/storage/queries", () => ({
	useAdoptStorage: () => adopt,
}));
vi.mock("@/components/ui/timestamp", () => ({
	TimestampValue: ({ iso }: { iso: string }) =>
		createElement("time", null, iso),
}));

import { StorageCard } from "./StorageCard";

afterEach(() => {
	cleanup();
	vi.clearAllMocks();
});

function details(state: StorageState, reason = "") {
	return {
		state,
		reason,
		backend: "local",
		location: "/mnt/data",
		storage_id: "abc123",
		checked_at: "2026-09-08T12:00:00Z",
	};
}

describe("StorageCard", () => {
	it("shows the facts and no action while attached", () => {
		render(createElement(StorageCard, { data: details("attached") }));
		expect(screen.getByTestId("storage-state").textContent).toBe(
			"storage.state_attached",
		);
		expect(screen.getByText("/mnt/data")).toBeTruthy();
		expect(screen.getByText("abc123")).toBeTruthy();
		expect(screen.getByText("storage.attached_hint")).toBeTruthy();
		expect(screen.queryByText("storage.adopt")).toBeNull();
	});

	it("explains an unreachable volume without offering to adopt it", () => {
		render(
			createElement(StorageCard, {
				data: details("unreachable", "storage unreachable: stat /mnt/data"),
			}),
		);
		expect(screen.getByText("storage.unreachable_hint")).toBeTruthy();
		expect(screen.getByText(/stat \/mnt\/data/)).toBeTruthy();
		expect(screen.queryByText("storage.adopt")).toBeNull();
	});

	it("adopts only after a confirm that names the location, then reports the scan", async () => {
		adopt.mutateAsync.mockResolvedValue({ scan_status: "scheduled" });
		render(
			createElement(StorageCard, {
				data: details("unattached", "marker missing"),
			}),
		);
		fireEvent.click(screen.getByText("storage.adopt"));
		expect(adopt.mutateAsync).not.toHaveBeenCalled();
		expect(
			screen.getByText("storage.adopt_description:/mnt/data"),
		).toBeTruthy();
		fireEvent.click(screen.getByText("storage.adopt_confirm"));
		await waitFor(() => expect(adopt.mutateAsync).toHaveBeenCalledTimes(1));
		await waitFor(() =>
			expect(toast.success).toHaveBeenCalledWith("storage.adopted_toast"),
		);
	});

	it("keeps the dialog open and reports a failed adopt", async () => {
		adopt.mutateAsync.mockRejectedValue(new Error("storage is unreachable"));
		render(createElement(StorageCard, { data: details("unattached", "x") }));
		fireEvent.click(screen.getByText("storage.adopt"));
		fireEvent.click(screen.getByText("storage.adopt_confirm"));
		await waitFor(() =>
			expect(toast.error).toHaveBeenCalledWith("storage is unreachable"),
		);
		expect(screen.getByText("storage.adopt_confirm")).toBeTruthy();
	});

	it("tells the difference between a scheduled scan and none", async () => {
		adopt.mutateAsync.mockResolvedValue({ scan_status: "disabled" });
		render(createElement(StorageCard, { data: details("unattached", "x") }));
		fireEvent.click(screen.getByText("storage.adopt"));
		fireEvent.click(screen.getByText("storage.adopt_confirm"));
		await waitFor(() =>
			expect(toast.success).toHaveBeenCalledWith(
				"storage.adopted_toast_no_scan",
			),
		);
	});
});

it("reports a scheduling failure without claiming scanning is disabled", async () => {
	adopt.mutateAsync.mockResolvedValue({ scan_status: "failed" });
	render(createElement(StorageCard, { data: details("unattached", "x") }));
	fireEvent.click(screen.getByText("storage.adopt"));
	fireEvent.click(screen.getByText("storage.adopt_confirm"));
	await waitFor(() =>
		expect(toast.warning).toHaveBeenCalledWith(
			"storage.adopted_toast_scan_failed",
		),
	);
});
