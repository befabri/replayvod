// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { type ComponentType, createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Role } from "@/api/generated/trpc";

const mockData = vi.hoisted(() => ({
	schedule: {
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
	},
}));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@tanstack/react-router", () => ({
	Link: ({ children, to }: { children: ReactNode; to: string }) =>
		createElement("a", { href: to }, children),
	createLink:
		(Component: ComponentType<Record<string, unknown>>) =>
		(props: Record<string, unknown>) =>
			createElement(Component, props),
}));
vi.mock("@/features/channels", () => ({
	useChannel: () => ({ data: { broadcaster_name: "Channel One" } }),
}));
vi.mock("@/features/schedules/queries", () => ({
	useMineSchedules: () => ({
		data: { data: [mockData.schedule] },
		isLoading: false,
		isError: false,
	}),
}));
vi.mock("@/features/schedules/components/EditForm", () => ({
	EditForm: () => createElement("div", { "data-testid": "edit-form" }),
}));
// Neutralize the trpc client side-effect pulled in transitively by the auth
// store; the store state itself is what we drive.
vi.mock("@/integrations/tanstack-query/root-provider", () => ({
	trpcClient: {},
}));

import { clearUser, setUser } from "@/stores/auth";
import { ScheduleStatistics } from "./ScheduleStatistics";

function login(role: Role) {
	setUser({ id: "u", login: "u", displayName: "U", role });
}

afterEach(() => {
	cleanup();
	clearUser();
});

describe("ScheduleStatistics role gating", () => {
	it("does not expose schedule editing to viewers", () => {
		login("viewer");
		render(createElement(ScheduleStatistics));
		expect(screen.getByText("Channel One")).toBeTruthy();
		expect(screen.queryByRole("button", { name: "schedules.edit" })).toBeNull();
		expect(screen.queryByTestId("edit-form")).toBeNull();
	});

	it("exposes schedule editing to admins and owners", () => {
		for (const role of ["admin", "owner"] as const) {
			cleanup();
			clearUser();
			login(role);
			render(createElement(ScheduleStatistics));
			fireEvent.click(screen.getByRole("button", { name: "schedules.edit" }));
			expect(screen.getByTestId("edit-form")).toBeTruthy();
		}
	});
});
