// @vitest-environment jsdom
import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

const remove = vi.hoisted(() => ({
	mutate:
		vi.fn<
			(
				input: { id: number },
				options: { onSuccess: () => void; onError: (error: Error) => void },
			) => void
		>(),
	isPending: false,
}));
const toast = vi.hoisted(() => ({ error: vi.fn() }));
vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("sonner", () => ({ toast }));
vi.mock("@/features/videos", () => ({ useDeleteVideo: () => remove }));

import { RemoveVideoButton } from "./RemoveVideoButton";

afterEach(() => {
	cleanup();
	vi.clearAllMocks();
});

it.each([
	false,
	true,
])("reports a refused removal and allows retry (permanent=%s)", async (permanent) => {
	const onRemoved = vi.fn();
	remove.mutate.mockImplementationOnce((_input, options) =>
		options.onError(new Error("Recording is still being finalized")),
	);
	remove.mutate.mockImplementationOnce((_input, options) =>
		options.onSuccess(),
	);
	render(
		<RemoveVideoButton
			videoId={95}
			permanent={permanent}
			onRemoved={onRemoved}
		/>,
	);
	fireEvent.click(screen.getByRole("button", { name: "videos.remove" }));
	const dialog = screen.getByRole("dialog");
	expect(dialog.textContent).toContain(
		permanent
			? "videos.remove_permanent_confirm_body"
			: "videos.remove_confirm_body",
	);
	fireEvent.click(
		within(dialog).getByRole("button", { name: "videos.remove_confirm" }),
	);
	expect(toast.error).toHaveBeenCalledWith(
		"Recording is still being finalized",
	);
	expect(onRemoved).not.toHaveBeenCalled();
	expect(screen.getByRole("dialog")).toBeTruthy();
	fireEvent.click(
		within(dialog).getByRole("button", { name: "videos.remove_confirm" }),
	);
	await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
	expect(onRemoved).toHaveBeenCalledTimes(1);
	expect(remove.mutate.mock.calls.map(([input]) => input)).toEqual([
		{ id: 95 },
		{ id: 95 },
	]);
});

it("reports a translated fallback for errors without a message", () => {
	remove.mutate.mockImplementationOnce((_input, options) =>
		options.onError(new Error()),
	);
	render(<RemoveVideoButton videoId={95} />);
	fireEvent.click(screen.getByRole("button", { name: "videos.remove" }));
	fireEvent.click(
		screen.getByRole("button", { name: "videos.remove_confirm" }),
	);
	expect(toast.error).toHaveBeenCalledWith("videos.remove_failed");
});
