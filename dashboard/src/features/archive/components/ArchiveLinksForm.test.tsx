// @vitest-environment jsdom

import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { EnqueueArchiveResponse } from "@/api/generated/trpc";
import { DEFAULT_ARCHIVE_SETTINGS } from "@/features/archive/settings";

const mutation = vi.hoisted(() => ({
	mutateAsync: vi.fn<(input: unknown) => Promise<EnqueueArchiveResponse>>(),
	isPending: false,
}));
const toast = vi.hoisted(() => ({
	success: vi.fn(),
	info: vi.fn(),
	error: vi.fn(),
}));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({
		t: (key: string, vars?: Record<string, unknown>) =>
			typeof vars === "object" && vars !== null && "count" in vars
				? `${key}:${vars.count}`
				: key,
	}),
}));
vi.mock("sonner", () => ({ toast }));
vi.mock("@/features/archive/queries", () => ({
	useEnqueueArchive: () => mutation,
}));
vi.mock("@tanstack/react-router", () => ({
	Link: ({ children }: { children?: ReactNode }) =>
		createElement("a", null, children),
}));

import { ArchiveLinksForm } from "./ArchiveLinksForm";

afterEach(() => {
	cleanup();
	vi.clearAllMocks();
});

function typeLinks(text: string) {
	fireEvent.change(screen.getByLabelText("archive.links_label"), {
		target: { value: text },
	});
}

describe("ArchiveLinksForm", () => {
	it("keeps submit disabled until a VOD link is present", () => {
		render(
			createElement(ArchiveLinksForm, { settings: DEFAULT_ARCHIVE_SETTINGS }),
		);
		const submit = screen.getByRole("button", { name: "archive.submit" });
		expect(submit.hasAttribute("disabled")).toBe(true);
		typeLinks("not a link");
		expect(screen.getByText("archive.links_invalid:1")).toBeTruthy();
		expect(submit.hasAttribute("disabled")).toBe(true);
	});

	it("explains the blocking limit even when some lines are invalid", () => {
		render(<ArchiveLinksForm settings={DEFAULT_ARCHIVE_SETTINGS} />);
		typeLinks(
			[...Array.from({ length: 51 }, (_, i) => String(i + 1)), "prose"].join(
				"\n",
			),
		);
		expect(screen.getByText("archive.links_too_many")).toBeTruthy();
		expect(
			screen
				.getByRole("button", { name: "archive.submit_count:51" })
				.hasAttribute("disabled"),
		).toBe(true);
	});

	it("counts valid links and flags the invalid ones before submit", () => {
		render(
			createElement(ArchiveLinksForm, { settings: DEFAULT_ARCHIVE_SETTINGS }),
		);
		typeLinks("https://www.twitch.tv/videos/1\nnope\n2");
		expect(screen.getByText("archive.links_invalid:1")).toBeTruthy();
		const submit = screen.getByRole("button", {
			name: "archive.submit_count:2",
		});
		expect(submit.hasAttribute("disabled")).toBe(false);
	});

	it("submits the valid inputs with the page settings and lists every outcome", async () => {
		mutation.mutateAsync.mockResolvedValue({
			items: [
				{
					input: "https://www.twitch.tv/videos/1",
					vod_id: "1",
					status: "queued",
					title: "first",
				},
				{
					input: "2",
					vod_id: "2",
					status: "exists",
					title: "held",
					video_id: 9,
				},
				{ input: "3", vod_id: "3", status: "not_found", message: "gone" },
			],
		});
		render(
			createElement(ArchiveLinksForm, {
				settings: {
					recording_type: "audio",
					quality: "BEST",
					force_h264: true,
				},
			}),
		);
		typeLinks("https://www.twitch.tv/videos/1\n2\nbad line\n3");
		fireEvent.click(
			screen.getByRole("button", { name: "archive.submit_count:3" }),
		);

		await waitFor(() => expect(mutation.mutateAsync).toHaveBeenCalledTimes(1));
		// Audio clears force_h264 on the client too; the server applies the same rule.
		expect(mutation.mutateAsync).toHaveBeenCalledWith({
			vods: ["https://www.twitch.tv/videos/1", "2", "3"],
			recording_type: "audio",
			quality: "BEST",
			force_h264: false,
		});
		await waitFor(() => {
			expect(screen.getByText("archive.result_queued")).toBeTruthy();
			expect(screen.getByText("archive.result_exists")).toBeTruthy();
			expect(screen.getByText("archive.result_not_found")).toBeTruthy();
			// The locally rejected line is reported next to the server outcomes.
			expect(screen.getByText("archive.result_invalid")).toBeTruthy();
		});
		expect(toast.success).toHaveBeenCalledWith("archive.queued_toast:1");
		// The box is cleared for the next paste; results stay on screen.
		expect(
			(screen.getByLabelText("archive.links_label") as HTMLTextAreaElement)
				.value,
		).toBe("");
		expect(screen.getByText("first")).toBeTruthy();
	});

	it("reports a whole-call failure as a toast and keeps the text", async () => {
		mutation.mutateAsync.mockRejectedValue(new Error("Twitch is down"));
		render(
			createElement(ArchiveLinksForm, { settings: DEFAULT_ARCHIVE_SETTINGS }),
		);
		typeLinks("123");
		fireEvent.click(
			screen.getByRole("button", { name: "archive.submit_count:1" }),
		);
		await waitFor(() =>
			expect(toast.error).toHaveBeenCalledWith("Twitch is down"),
		);
		expect(
			(screen.getByLabelText("archive.links_label") as HTMLTextAreaElement)
				.value,
		).toBe("123");
	});

	it("tells the user when nothing new was queued", async () => {
		mutation.mutateAsync.mockResolvedValue({
			items: [{ input: "5", vod_id: "5", status: "exists" }],
		});
		render(
			createElement(ArchiveLinksForm, { settings: DEFAULT_ARCHIVE_SETTINGS }),
		);
		typeLinks("5");
		fireEvent.click(
			screen.getByRole("button", { name: "archive.submit_count:1" }),
		);
		await waitFor(() =>
			expect(toast.info).toHaveBeenCalledWith("archive.nothing_queued_toast"),
		);
	});
});
