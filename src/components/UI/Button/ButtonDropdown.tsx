import React, { useState } from "react";
import { Icon } from "@iconify/react";

interface DropdownButtonProps {
    label: string;
    options: string[];
    onOptionSelected: (value: string) => void;
}
const DropdownButton: React.FC<DropdownButtonProps> = ({ label, options, onOptionSelected }) => {
    const [isOpen, setIsOpen] = useState(false);

    const handleToggle = () => {
        setIsOpen(!isOpen);
    };

    const handleSelect = (value: any) => {
        onOptionSelected(value);
        setIsOpen(false);
    };
    return (
        <div className="relative inline-block text-left z-10 dark:bg-gray-800">
            <div>
                <button
                    type="button"
                    className="inline-flex justify-center w-full rounded-md border border-gray-300 shadow-sm px-2 py-2 bg-white text-sm font-medium text-gray-700 hover:bg-gray-50 focus:outline-none dark:bg-gray-700 dark:text-gray-200 dark:hover:bg-gray-600 dark:border-gray-600"
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
                    className="origin-top-right absolute right-0 mt-2 w-36 rounded-md shadow-lg bg-white ring-1 ring-black ring-opacity-5 focus:outline-none dark:bg-gray-700 dark:ring-gray-600"
                    role="menu"
                    aria-orientation="vertical"
                    aria-labelledby="options-menu">
                    <div className="py-1" role="none">
                        {options.map((option, index) => (
                            <a
                                key={index}
                                href="#"
                                className="block px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-200 dark:hover:bg-gray-600"
                                role="menuitem"
                                onClick={() => handleSelect(option)}>
                                {option}
                            </a>
                        ))}
                    </div>
                </div>
            )}
        </div>
    );
};

export default DropdownButton;
