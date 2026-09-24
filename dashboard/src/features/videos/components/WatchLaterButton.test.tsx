// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { WatchLaterButton } from "./WatchLaterButton";

const mutationMock = vi.hoisted(() => ({ isPending: false, mutate: vi.fn() }));
const mutateMock = mutationMock.mutate;

vi.mock("react-i18next", () => ({
	useTranslation: () => ({
		t: (key: string) => key,
	}),
}));

vi.mock("@/features/videos", () => ({
	useSetWatchLater: () => mutationMock,
}));

afterEach(() => {
	cleanup();
	mutateMock.mockReset();
	mutationMock.isPending = false;
});

describe("WatchLaterButton", () => {
	it("adds a video to watch later", () => {
		render(<WatchLaterButton videoId={42} watchLater={false} />);

		fireEvent.click(screen.getByRole("button"));

		expect(mutateMock).toHaveBeenCalledWith(
			{ video_id: 42, watch_later: true },
			expect.objectContaining({
				onError: expect.any(Function),
				onSuccess: expect.any(Function),
			}),
		);
	});

	it("removes a video from watch later", () => {
		render(<WatchLaterButton videoId={42} watchLater />);

		fireEvent.click(screen.getByRole("button"));

		expect(mutateMock).toHaveBeenCalledWith(
			{ video_id: 42, watch_later: false },
			expect.any(Object),
		);
	});

	// The name stays the same in both states so a screen reader announces the
	// control and its pressed state, never the action it would take next.
	it.each([
		[false, "false", "videos.watch_later.add"],
		[true, "true", "videos.watch_later.remove"],
	])("keeps one name when watchLater is %s", (watchLater, pressed, hint) => {
		render(<WatchLaterButton videoId={42} watchLater={watchLater} withLabel />);

		const button = screen.getByRole("button", {
			name: "videos.watch_later.label",
		});
		expect(button.getAttribute("aria-pressed")).toBe(pressed);
		expect(button.getAttribute("title")).toBe(hint);
		expect(button.textContent).toBe("videos.watch_later.label");
	});

	it("keeps its name and pressed state when toggled", () => {
		render(<WatchLaterButton videoId={42} watchLater={false} />);
		const button = screen.getByRole("button", {
			name: "videos.watch_later.label",
		});

		fireEvent.click(button);

		expect(button.getAttribute("aria-label")).toBe("videos.watch_later.label");
		expect(button.getAttribute("aria-pressed")).toBe("true");
		expect(button.getAttribute("title")).toBe("videos.watch_later.remove");
	});

	// A disabled button drops keyboard focus while the request is in flight;
	// the toggle stays focusable and ignores clicks until it settles.
	it("stays focusable and ignores clicks while a change is pending", () => {
		mutationMock.isPending = true;
		render(<WatchLaterButton videoId={42} watchLater={false} />);
		const button = screen.getByRole("button", {
			name: "videos.watch_later.label",
		});

		button.focus();
		fireEvent.click(button);

		expect(button.hasAttribute("disabled")).toBe(false);
		expect(document.activeElement).toBe(button);
		expect(mutateMock).not.toHaveBeenCalled();
		expect(button.getAttribute("aria-pressed")).toBe("false");
	});
});
