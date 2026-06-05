// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { createElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Role } from "@/api/generated/trpc";

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@/features/schedules/queries", () => ({
	useSchedulesPaused: () => ({ data: { paused: true } }),
	useSetSchedulesPaused: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));
// Neutralize the trpc client side-effect pulled in transitively by the auth
// store; the store state itself is what we drive.
vi.mock("@/integrations/tanstack-query/root-provider", () => ({
	trpcClient: {},
}));

import { clearUser, setUser } from "@/stores/auth";
import { SchedulesPausedBanner } from "./SchedulesPausedBanner";

function login(role: Role) {
	setUser({ id: "u", login: "u", displayName: "U", role });
}

afterEach(() => {
	cleanup();
	clearUser();
});

describe("SchedulesPausedBanner role gating", () => {
	it("shows the paused text but hides Resume from viewers", () => {
		login("viewer");
		render(createElement(SchedulesPausedBanner));
		expect(screen.getByText("schedules.paused_banner_title")).toBeTruthy();
		expect(screen.queryByText("schedules.resume_all")).toBeNull();
	});

	it("offers Resume to admins", () => {
		login("admin");
		render(createElement(SchedulesPausedBanner));
		expect(screen.getByText("schedules.resume_all")).toBeTruthy();
	});
});
