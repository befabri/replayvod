import React, { useRef, useState } from "react";
import { Icon } from "@iconify/react";
import useOutsideClick from "../../../hooks/useOutsideClick";

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
    const dropdownRef = useRef<HTMLDivElement>(null);

    useOutsideClick(dropdownRef, () => setIsOpen(false));

    const handleToggle = () => {
        setIsOpen(!isOpen);
    };

    const handleSelect = (option: DropdownOption) => {
        onOptionSelected(option.value);
        setIsOpen(false);
    };
    
    return (
        <div ref={dropdownRef} className="relative z-10 inline-block text-left dark:bg-custom_lightblue">
            <button
                type="button"
                className="inline-flex w-full justify-center rounded-md border border-gray-300 bg-white px-2 py-2 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50 focus:outline-none dark:border-custom_lightblue dark:bg-custom_lightblue dark:text-gray-200 dark:hover:bg-custom_vista_blue"
                id="options-menu"
                aria-expanded="true"
                aria-haspopup="true"
                onClick={handleToggle}>
                {label}
                <Icon icon="mdi:chevron-down" width="18" height="18" className="ml-2" />
            </button>
            {isOpen && (
                <div
                    className="absolute right-0 mt-2 w-36 origin-top-right rounded-md bg-white shadow-lg ring-1 ring-black ring-opacity-5 focus:outline-none dark:bg-custom_space_cadet dark:ring-custom_space_cadet"
                    role="menu"
                    aria-orientation="vertical"
                    aria-labelledby="options-menu">
                    <div className="" role="none">
                        {options.map((option, index) => (
                            <button
                                key={index}
                                className="block w-full px-2 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-200 dark:hover:bg-custom_vista_blue"
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
