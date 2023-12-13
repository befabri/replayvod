import type { Meta, StoryObj } from "@storybook/react";
import DarkModeToggle from "./DarkModeToggle";

const meta: Meta = {
    title: "UI/DarkModeToggle",
    component: DarkModeToggle,
    parameters: {
        layout: "centered",
    },
    tags: ["autodocs"],
} satisfies Meta<typeof DarkModeToggle>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {
    args: {
        text: "Toggle Dark Mode",
    },
};

export const Off: Story = {
    args: {
        text: "Toggle Dark Mode",
    },
};

export const WithCustomClass: Story = {
    args: {
        className: "p-4 bg-gray-100 rounded-lg",
        text: "Dark Mode",
    },
};

export const WithLargeText: Story = {
    args: {
        className: "text-lg",
        text: "Dark Mode",
    },
};

export const WithDifferentText: Story = {
    args: {
        text: "Switch Theme",
    },
};
