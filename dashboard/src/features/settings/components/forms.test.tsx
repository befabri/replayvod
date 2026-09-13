// @vitest-environment jsdom

import {
	act,
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { SettingsResponse } from "@/api/generated/trpc";
import { USER_SETTINGS } from "@/test/playback-settings";

const save = vi.hoisted(() => vi.fn());
vi.mock("../queries", () => ({
	useUpdatePlaybackSettings: () => ({ mutateAsync: save, isSuccess: true }),
}));
vi.mock("@/features/settings", () => ({
	useUpdateSettings: () => ({ mutateAsync: save, isSuccess: true }),
}));
vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));

import { PlaybackSettingsForm } from "./PlaybackSettingsForm";
import { SettingsForm } from "./SettingsForm";

afterEach(() => {
	cleanup();
	vi.clearAllMocks();
});

it.each([
	"playback",
	"locale",
] as const)("preserves %s edits made while the previous values are saving", async (kind) => {
	let finish!: (saved: SettingsResponse) => void;
	save.mockImplementation(
		() =>
			new Promise<SettingsResponse>((resolve) => {
				finish = resolve;
			}),
	);
	render(
		kind === "playback" ? (
			<PlaybackSettingsForm data={USER_SETTINGS} />
		) : (
			<SettingsForm data={USER_SETTINGS} />
		),
	);
	const input = screen.getByLabelText(
		kind === "playback" ? "settings.resume_min_seconds" : "settings.timezone",
	);
	const submitted = kind === "playback" ? "20" : "Europe/Brussels";
	const next = kind === "playback" ? "30" : "Europe/Paris";
	fireEvent.change(input, { target: { value: submitted } });
	fireEvent.click(screen.getByRole("button", { name: "settings.save" }));
	await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
	fireEvent.change(input, { target: { value: next } });
	const saved =
		kind === "playback"
			? {
					...USER_SETTINGS,
					playback: { ...USER_SETTINGS.playback, resume_min_seconds: 20 },
				}
			: { ...USER_SETTINGS, timezone: submitted };
	await act(async () => {
		finish(saved);
	});
	expect((input as HTMLInputElement).value).toBe(next);
	expect(screen.queryByRole("status")).toBeNull();
	fireEvent.click(screen.getByRole("button", { name: "settings.save" }));
	await waitFor(() => expect(save).toHaveBeenCalledTimes(2));
	expect(save.mock.calls[1][0]).toMatchObject(
		kind === "playback" ? { resume_min_seconds: 30 } : { timezone: next },
	);
	await act(async () => {
		finish(saved);
	});
});

it.each([
	"playback",
	"locale",
] as const)("clears the %s saved notice when there are new edits", (kind) => {
	render(
		kind === "playback" ? (
			<PlaybackSettingsForm data={USER_SETTINGS} />
		) : (
			<SettingsForm data={USER_SETTINGS} />
		),
	);
	expect(screen.getByRole("status").textContent).toBe("settings.saved");
	fireEvent.change(
		screen.getByLabelText(
			kind === "playback" ? "settings.resume_min_seconds" : "settings.timezone",
		),
		{
			target: { value: kind === "playback" ? "20" : "Europe/Brussels" },
		},
	);
	expect(screen.queryByRole("status")).toBeNull();
});

it.each([
	["playback", "before"],
	["playback", "after"],
	["locale", "before"],
	["locale", "after"],
] as const)("preserves %s edits back to the original value when saved props arrive %s completion", async (kind, responseTiming) => {
	let finish!: (saved: SettingsResponse) => void;
	save.mockImplementation(
		() =>
			new Promise<SettingsResponse>((resolve) => {
				finish = resolve;
			}),
	);
	const formWithData = (data: SettingsResponse) =>
		kind === "playback" ? (
			<PlaybackSettingsForm data={data} />
		) : (
			<SettingsForm data={data} />
		);
	const { rerender } = render(formWithData(USER_SETTINGS));
	const input = screen.getByLabelText(
		kind === "playback" ? "settings.resume_min_seconds" : "settings.timezone",
	);
	const original = kind === "playback" ? "5" : "UTC";
	const submitted = kind === "playback" ? "10" : "Europe/Brussels";
	fireEvent.change(input, { target: { value: submitted } });
	fireEvent.click(screen.getByRole("button", { name: "settings.save" }));
	await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
	fireEvent.change(input, { target: { value: original } });
	const saved =
		kind === "playback"
			? {
					...USER_SETTINGS,
					playback: { ...USER_SETTINGS.playback, resume_min_seconds: 10 },
				}
			: { ...USER_SETTINGS, timezone: submitted };
	// onSuccess publishes the cache before mutateAsync completes, but the
	// query observer may render that snapshot on either side of completion.
	if (responseTiming === "before") rerender(formWithData(saved));
	await act(async () => {
		finish(saved);
	});
	if (responseTiming === "after") rerender(formWithData(saved));
	expect((input as HTMLInputElement).value).toBe(original);
	expect(screen.queryByRole("status")).toBeNull();

	fireEvent.click(screen.getByRole("button", { name: "settings.save" }));
	await waitFor(() => expect(save).toHaveBeenCalledTimes(2));
	expect(save.mock.calls[1][0]).toMatchObject(
		kind === "playback" ? { resume_min_seconds: 5 } : { timezone: "UTC" },
	);
	rerender(formWithData(USER_SETTINGS));
	await act(async () => {
		finish(USER_SETTINGS);
	});
	expect((input as HTMLInputElement).value).toBe(original);
	expect(screen.getByRole("status").textContent).toBe("settings.saved");

	// After that save is acknowledged, the form is pristine again and accepts
	// a subsequent server refresh instead of remaining permanently dirty.
	rerender(formWithData(saved));
	expect((input as HTMLInputElement).value).toBe(submitted);
});
