// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { EnqueueArchiveItem } from "@/api/generated/trpc";

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@tanstack/react-router", () => ({
	Link: ({ children }: { children?: ReactNode }) =>
		createElement("a", null, children),
}));

import { EnqueueResults } from "./EnqueueResults";

afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
});

describe("EnqueueResults", () => {
	it("renders nothing for an empty result set", () => {
		const { container } = render(createElement(EnqueueResults, { items: [] }));
		expect(container.innerHTML).toBe("");
	});

	it("keys duplicate inputs uniquely so React does not warn", () => {
		const consoleError = vi
			.spyOn(console, "error")
			.mockImplementation(() => {});
		const items: EnqueueArchiveItem[] = [
			{
				input: "https://www.twitch.tv/videos/1",
				status: "queued",
				title: "one",
			},
			{
				input: "https://www.twitch.tv/videos/1",
				status: "exists",
				title: "one",
				video_id: 9,
			},
			{ input: "2", status: "invalid", message: "not a vod" },
		];
		render(createElement(EnqueueResults, { items }));

		expect(screen.getAllByRole("listitem")).toHaveLength(3);
		expect(screen.getByText("archive.result_queued")).toBeTruthy();
		expect(screen.getByText("archive.result_exists")).toBeTruthy();
		expect(screen.getByText("archive.result_invalid")).toBeTruthy();
		expect(screen.getByText("archive.open_recording")).toBeTruthy();

		const keyWarnings = consoleError.mock.calls.filter((call) =>
			call.some(
				(arg) =>
					typeof arg === "string" &&
					arg.includes("Encountered two children with the same key"),
			),
		);
		expect(keyWarnings).toEqual([]);
		expect(consoleError).not.toHaveBeenCalled();
	});
});
