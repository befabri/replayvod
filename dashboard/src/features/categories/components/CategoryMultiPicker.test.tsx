// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CategoryResponse } from "@/api/generated/trpc";
import {
	useCategories,
	useCategorySearch,
} from "@/features/categories/queries";
import { CategoryMultiPicker } from "./CategoryMultiPicker";

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("@/features/categories/queries", () => ({
	useCategories: vi.fn(),
	useCategorySearch: vi.fn(),
}));

const existingCategory: CategoryResponse = {
	id: "cat-existing",
	name: "Existing Game",
	created_at: "2026-06-05T12:00:00Z",
	updated_at: "2026-06-05T12:00:00Z",
};

beforeEach(() => {
	vi.mocked(useCategories).mockReturnValue({ data: undefined } as ReturnType<
		typeof useCategories
	>);
	vi.mocked(useCategorySearch).mockReturnValue({
		data: undefined,
		isFetching: false,
	} as ReturnType<typeof useCategorySearch>);
});

describe("CategoryMultiPicker", () => {
	it("keeps selected schedule categories visible before the catalog loads", () => {
		render(
			<CategoryMultiPicker
				selected={[existingCategory.id]}
				selectedCategories={[existingCategory]}
				onChange={vi.fn()}
			/>,
		);

		expect(screen.getByText("Existing Game")).toBeTruthy();
	});
});
