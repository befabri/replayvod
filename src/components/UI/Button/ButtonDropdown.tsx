import React, { useState } from "react";
import { Icon } from "@iconify/react";

interface DropdownOption {
    value: string;
    label: string;
}

interface DropdownButtonProps {
    label: string;
    options: DropdownOption[];
    onOptionSelected: (value: string) => void;
}
const DropdownButton: React.FC<DropdownButtonProps> = ({ label, options, onOptionSelected }) => {
    const [isOpen, setIsOpen] = useState(false);

    const handleToggle = () => {
        setIsOpen(!isOpen);
    };

    const handleSelect = (option: DropdownOption) => {
        onOptionSelected(option.value);
        setIsOpen(false);
    };
    return (
        <div className="relative inline-block text-left z-10 dark:bg-custom_lightblue">
            <div>
                <button
                    type="button"
                    className="inline-flex justify-center w-full rounded-md border border-gray-300 shadow-sm px-2 py-2 bg-white text-sm font-medium text-gray-700 hover:bg-gray-50 focus:outline-none dark:bg-custom_lightblue dark:text-gray-200 dark:hover:bg-custom_vista_blue dark:border-custom_lightblue"
                    id="options-menu"
                    aria-expanded="true"
                    aria-haspopup="true"
                    onClick={handleToggle}>
                    {label}
                    <Icon icon="mdi:chevron-down" width="18" height="18" className="ml-2" />
                </button>
            </div>

            {isOpen && (
                <div
                    className="origin-top-right absolute right-0 mt-2 w-36 rounded-md shadow-lg bg-white ring-1 ring-black ring-opacity-5 focus:outline-none dark:bg-custom_space_cadet dark:ring-custom_space_cadet"
                    role="menu"
                    aria-orientation="vertical"
                    aria-labelledby="options-menu">
                    <div className="" role="none">
                        {options.map((option, index) => (
                            <button
                                key={index}
                                className="text-left w-full block px-2 py-2 text-sm text-gray-700 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-200 dark:hover:bg-custom_vista_blue"
                                role="menuitem"
                                onClick={() => handleSelect(option)}>
                                {option.label}
                            </button>
                        ))}
                    </div>
                </div>
            )}
        </div>
    );
};

export default DropdownButton;
