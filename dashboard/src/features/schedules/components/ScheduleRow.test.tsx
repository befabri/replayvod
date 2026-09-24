// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { createElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ScheduleResponse } from "@/api/generated/trpc";

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@/features/schedules/queries", () => ({
	useToggleSchedule: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));
const channelQuery = vi.hoisted(() => ({
	current: { data: undefined } as Record<string, unknown>,
}));
vi.mock("@/features/channels", () => ({
	useChannel: () => channelQuery.current,
}));
// EditForm pulls a deep dependency graph (category/tag pickers); stub it since
// it only mounts inside the (closed) edit dialog and isn't under test here.
vi.mock("./EditForm", () => ({ EditForm: () => null }));
vi.mock("@/components/ui/avatar", () => ({
	Avatar: ({ name }: { name: string }) => createElement("span", null, name),
}));

import { ScheduleRow } from "./ScheduleRow";

function schedule(partial: Partial<ScheduleResponse> = {}): ScheduleResponse {
	return {
		id: 1,
		broadcaster_id: "b-1",
		requested_by: "u-1",
		requested_from_name: "",
		recording_type: "video",
		quality: "HIGH",
		force_h264: false,
		has_min_viewers: false,
		has_categories: false,
		has_tags: false,
		is_delete_rediff: false,
		is_disabled: false,
		trigger_count: 0,
		created_at: "2026-06-01T00:00:00Z",
		updated_at: "2026-06-01T00:00:00Z",
		categories: [],
		tags: [],
		...partial,
	};
}

afterEach(() => {
	cleanup();
	channelQuery.current = { data: undefined };
});

describe("ScheduleRow role gating", () => {
	it("shows the original requester on an approved schedule", () => {
		render(
			createElement(ScheduleRow, {
				schedule: schedule({
					requested_by: "admin",
					requested_from: "viewer",
					requested_from_name: "Vera Viewer",
				}),
				canManage: false,
			}),
		);
		expect(
			screen.getByText(/schedules.requested_by: Vera Viewer/),
		).toBeTruthy();
	});

	it("omits request attribution for a directly created schedule", () => {
		render(createElement(ScheduleRow, { schedule: schedule() }));
		expect(screen.queryByText(/schedules.requested_by/)).toBeNull();
	});
	it("hides edit and disables the toggle for read-only viewers", () => {
		render(
			createElement(ScheduleRow, { schedule: schedule(), canManage: false }),
		);
		expect(screen.queryByRole("button", { name: "schedules.edit" })).toBeNull();
		const toggle = screen.getByRole("button", { name: "schedules.disable" });
		expect((toggle as HTMLButtonElement).disabled).toBe(true);
	});

	it("exposes edit and an enabled toggle for admins", () => {
		render(
			createElement(ScheduleRow, { schedule: schedule(), canManage: true }),
		);
		expect(screen.getByRole("button", { name: "schedules.edit" })).toBeTruthy();
		const toggle = screen.getByRole("button", { name: "schedules.disable" });
		expect((toggle as HTMLButtonElement).disabled).toBe(false);
	});
});

describe("ScheduleRow channel loading", () => {
	it("holds the skeleton while the first channel request is in flight", () => {
		channelQuery.current = {
			data: undefined,
			isPending: true,
			fetchStatus: "fetching",
			failureCount: 0,
		};
		render(createElement(ScheduleRow, { schedule: schedule() }));
		expect(
			screen.queryByRole("button", { name: "schedules.disable" }),
		).toBeNull();
		expect(screen.queryByText("b-1")).toBeNull();
	});

	// A failed lookup is retried with backoff for several seconds. The row
	// must stay usable meanwhile, as it does when the lookup is disabled or
	// waiting for the network.
	it.each([
		{ fetchStatus: "fetching", failureCount: 1 },
		{ fetchStatus: "idle", failureCount: 0 },
		{ fetchStatus: "paused", failureCount: 0 },
	])("shows the channel id while $fetchStatus after $failureCount failures", ({
		fetchStatus,
		failureCount,
	}) => {
		channelQuery.current = {
			data: undefined,
			isPending: true,
			fetchStatus,
			failureCount,
		};
		render(createElement(ScheduleRow, { schedule: schedule() }));
		expect(screen.getAllByText("b-1").length).toBeGreaterThan(0);
		const toggle = screen.getByRole("button", { name: "schedules.disable" });
		expect((toggle as HTMLButtonElement).disabled).toBe(false);
	});
});
