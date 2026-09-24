// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { TaskResponse } from "@/features/tasks";

const actions = vi.hoisted(() => ({ toggle: vi.fn(), run: vi.fn() }));
vi.mock("@/features/tasks", () => ({
	useToggleTask: () => ({ mutate: actions.toggle, isPending: false }),
	useRunTaskNow: () => ({ mutate: actions.run, isPending: false }),
}));
vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));

import { TaskActions } from "./TaskActions";

afterEach(() => {
	cleanup();
	vi.clearAllMocks();
});

it("prevents unavailable work while letting the operator preserve a pause", () => {
	const task: TaskResponse = {
		name: "recordings_retention",
		description: "Retention",
		interval_seconds: 60,
		is_enabled: true,
		is_available: false,
		last_status: "success",
		last_duration_ms: 0,
		created_at: "2026-01-01T00:00:00Z",
		updated_at: "2026-01-01T00:00:00Z",
	};
	const { rerender } = render(<TaskActions task={task} />);
	const run = screen.getByRole("button", { name: "tasks.run_now" });
	expect(run.getAttribute("aria-disabled")).toBe("true");
	expect(run.tabIndex).toBe(0);
	const reason = document.getElementById(
		run.getAttribute("aria-describedby") ?? "",
	);
	expect(reason?.textContent).toBe("tasks.unavailable");
	fireEvent.click(run);
	expect(actions.run).not.toHaveBeenCalled();
	fireEvent.click(screen.getByRole("button", { name: "tasks.pause" }));
	expect(actions.toggle).toHaveBeenCalledWith({
		name: task.name,
		enabled: false,
	});
	rerender(
		<TaskActions task={{ ...task, is_available: true, is_enabled: false }} />,
	);
	expect(screen.getByRole("button", { name: "tasks.resume" })).toBeTruthy();
	const available = screen.getByRole("button", { name: "tasks.run_now" });
	expect(available.getAttribute("aria-disabled")).toBeNull();
	expect(available.getAttribute("aria-describedby")).toBeNull();
	fireEvent.click(available);
	expect(actions.run).toHaveBeenCalledWith({ name: task.name });
});
