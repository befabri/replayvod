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
vi.mock("@/features/channels", () => ({
	useChannel: () => ({ data: undefined }),
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
	} as ScheduleResponse;
}

afterEach(cleanup);

describe("ScheduleRow role gating", () => {
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
