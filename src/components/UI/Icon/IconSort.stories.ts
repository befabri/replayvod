import type { Meta, StoryObj } from "@storybook/react";
import IconSort from "./IconSort";

const meta: Meta = {
    title: "UI/IconSort",
    component: IconSort,
    parameters: {
        layout: "centered",
    },
    tags: ["autodocs"],
} satisfies Meta<typeof IconSort>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
    args: {},
};

export const WithCustomSize: Story = {
    args: {
        className: "w-6 h-6 ml-1",
    },
};

export const WithCustomColor: Story = {
    args: {
        className: "w-3 h-3 ml-1 text-blue-500",
    },
};