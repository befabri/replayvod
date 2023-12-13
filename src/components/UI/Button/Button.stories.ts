import type { Meta, StoryObj } from "@storybook/react";
import Button from "./Button";

const meta: Meta = {
    title: "UI/Button",
    component: Button,
    parameters: {
        layout: "centered",
    },
    tags: ["autodocs"],
    argTypes: {
        style: {
            control: { type: "select", options: ["primary", "svg"] },
        },
        disabled: {
            control: "boolean",
        },
    },
} satisfies Meta<typeof Button>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Primary: Story = {
    args: {
        text: "Button",
        style: "primary",
    },
};

export const Secondary: Story = {
    args: {
        text: "Button",
        style: "svg",
    },
};

export const Disabled: Story = {
    args: {
        text: "Button",
        disabled: true,
    },
};
