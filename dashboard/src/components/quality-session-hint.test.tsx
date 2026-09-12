// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Role } from "@/api/generated/trpc";
import type { RecordingMode, RecordingQuality } from "@/lib/recording-settings";

const status = vi.hoisted(() => ({
	data: undefined as { state: string } | undefined,
	reads: 0,
}));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@tanstack/react-router", () => ({
	Link: ({ children, to }: { children?: ReactNode; to: string }) =>
		createElement("a", { href: to }, children),
}));
// Neutralize the trpc client side-effect pulled in transitively by the auth
// store; the store state itself is what we drive.
vi.mock("@/integrations/tanstack-query/root-provider", () => ({
	trpcClient: {},
}));
vi.mock("@/features/system/queries", () => ({
	useTwitchPlaybackStatus: () => {
		status.reads++;
		return { data: status.data };
	},
}));

import { RecordingSettingsFields } from "@/components/recording-settings-fields";
import { clearUser, setUser } from "@/stores/auth";
import { QualitySessionHint } from "./quality-session-hint";

function login(role: Role) {
	setUser({ id: "u", login: "u", displayName: "U", role });
}

function hint() {
	return screen.queryByTestId("quality-session-hint");
}

afterEach(() => {
	cleanup();
	clearUser();
	status.data = undefined;
	status.reads = 0;
});

describe("QualitySessionHint", () => {
	it.each<RecordingQuality>([
		"HIGH",
		"MEDIUM",
		"LOW",
	])("stays silent for %s, which Twitch serves anonymously", (quality) => {
		login("owner");
		render(createElement(QualitySessionHint, { quality }));
		expect(hint()).toBeNull();
		expect(status.reads).toBe(0);
	});

	it.each<[Role, RecordingQuality]>([
		["viewer", "1440"],
		["admin", "BEST"],
	])("tells a %s the owner holds the session, without reading the status", (role, quality) => {
		login(role);
		render(createElement(QualitySessionHint, { quality }));
		expect(
			screen.getByText("twitch_playback.quality_session_viewer"),
		).toBeTruthy();
		expect(screen.queryByRole("link")).toBeNull();
		expect(status.reads).toBe(0);
	});

	it("warns the owner that no session is connected and links to the page", () => {
		login("owner");
		status.data = { state: "disconnected" };
		render(createElement(QualitySessionHint, { quality: "1440" }));
		expect(screen.getByRole("status").getAttribute("data-state")).toBe(
			"disconnected",
		);
		expect(
			screen.getByText(/twitch_playback\.quality_session_missing/),
		).toBeTruthy();
		expect(screen.getByRole("link").getAttribute("href")).toBe(
			"/dashboard/system/twitch",
		);
	});

	it("asks the owner to reconnect a rejected session", () => {
		login("owner");
		status.data = { state: "reconnect_required" };
		render(createElement(QualitySessionHint, { quality: "BEST" }));
		expect(
			screen.getByText(/twitch_playback\.quality_session_reconnect/),
		).toBeTruthy();
		expect(screen.getByRole("link")).toBeTruthy();
	});

	it("renders nothing for the owner while loading or once connected", () => {
		login("owner");
		render(createElement(QualitySessionHint, { quality: "1440" }));
		expect(hint()).toBeNull();
		cleanup();
		status.data = { state: "connected" };
		render(createElement(QualitySessionHint, { quality: "1440" }));
		expect(hint()).toBeNull();
		expect(status.reads).toBeGreaterThan(0);
	});
});

describe("RecordingSettingsFields quality hint", () => {
	function fields(props: {
		recordingType?: RecordingMode;
		quality?: RecordingQuality;
		disabled?: boolean;
	}) {
		return createElement(RecordingSettingsFields, {
			tBase: "videos",
			recordingType: props.recordingType ?? "video",
			onRecordingTypeChange: () => {},
			quality: props.quality ?? "1440",
			onQualityChange: () => {},
			forceH264: false,
			onForceH264Change: () => {},
			disabled: props.disabled,
		});
	}

	it("shows the hint under the picker for a quality above 1080p", () => {
		login("viewer");
		render(fields({}));
		expect(hint()).toBeTruthy();
	});

	it("hides it for 1080p and below", () => {
		login("viewer");
		render(fields({ quality: "HIGH" }));
		expect(hint()).toBeNull();
	});

	it("hides it while quality is greyed out (audio mode or offline)", () => {
		login("viewer");
		render(fields({ recordingType: "audio" }));
		expect(hint()).toBeNull();
		cleanup();
		render(fields({ disabled: true }));
		expect(hint()).toBeNull();
	});
});
