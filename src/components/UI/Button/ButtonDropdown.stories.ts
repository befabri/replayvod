import type { Meta, StoryObj } from "@storybook/react";
import DropdownButton from "./ButtonDropdown";

const meta: Meta = {
    title: "UI/ButtonDropdown",
    component: DropdownButton,
    parameters: {
        layout: "centered",
    },
    tags: ["autodocs"],
    argTypes: {
        label: {
            control: "text",
            defaultValue: "Dropdown Button",
        },
        options: {
            control: "array",
            defaultValue: ["Option 1", "Option 2", "Option 3"],
        },
        onOptionSelected: { action: "onOptionSelected" },
    },
} satisfies Meta<typeof DropdownButton>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {
    args: {
        label: "Dropdown Button",
        options: ["Option 1", "Option 2", "Option 3"],
        onOptionSelected: (value: string) => console.log(value),
    },
};
