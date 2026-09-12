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

const restore = vi.hoisted(() => ({
	mutate:
		vi.fn<
			(
				input: { id: number },
				opts: { onSuccess?: () => void; onError?: (err: unknown) => void },
			) => void
		>(),
	isPending: false,
}));
const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("sonner", () => ({ toast }));
vi.mock("@/features/videos/queries", () => ({
	useRestoreVideo: () => restore,
}));

import { RestoreVideoButton } from "./RestoreVideoButton";

afterEach(() => {
	cleanup();
	vi.clearAllMocks();
});

describe("RestoreVideoButton", () => {
	it("restores without a confirm and reports success", async () => {
		const onRestored = vi.fn();
		restore.mutate.mockImplementation((_input, opts) => opts.onSuccess?.());
		render(createElement(RestoreVideoButton, { videoId: 7, onRestored }));
		fireEvent.click(screen.getByRole("button", { name: "videos.restore" }));
		await waitFor(() => expect(restore.mutate).toHaveBeenCalledTimes(1));
		expect(restore.mutate.mock.calls[0][0]).toEqual({ id: 7 });
		expect(toast.success).toHaveBeenCalledWith("videos.restored_toast");
		expect(onRestored).toHaveBeenCalledTimes(1);
	});

	it("shows the server's reason when the media is not all back", async () => {
		restore.mutate.mockImplementation((_input, opts) =>
			opts.onError?.(new Error("2 of 5 parts are still missing")),
		);
		render(createElement(RestoreVideoButton, { videoId: 7 }));
		fireEvent.click(screen.getByRole("button", { name: "videos.restore" }));
		await waitFor(() =>
			expect(toast.error).toHaveBeenCalledWith(
				"2 of 5 parts are still missing",
			),
		);
		expect(toast.success).not.toHaveBeenCalled();
	});

	it("does not bubble the click to a wrapping link", () => {
		restore.mutate.mockImplementation(() => {});
		const onLinkClick = vi.fn();
		render(
			createElement(
				"a",
				{ href: "/x", onClick: onLinkClick },
				createElement(RestoreVideoButton, { videoId: 7, withLabel: true }),
			),
		);
		fireEvent.click(screen.getByText("videos.restore"));
		expect(onLinkClick).not.toHaveBeenCalled();
		expect(restore.mutate).toHaveBeenCalledTimes(1);
	});
});
