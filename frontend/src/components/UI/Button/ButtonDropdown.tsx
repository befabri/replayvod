import React, { useRef, useState } from "react";
import { Icon } from "@iconify/react";
import useOutsideClick from "../../../hooks/useOutsideClick";

interface DropdownOption {
    value: string;
    label: string;
    icon: string;
}

interface DropdownButtonProps {
    label: string;
    options: DropdownOption[];
    onOptionSelected: (value: string) => void;
    icon: string;
}

const DropdownButton: React.FC<DropdownButtonProps> = ({ label, options, onOptionSelected, icon }) => {
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
        <div
            ref={dropdownRef}
            className="relative z-10 inline-block rounded-md text-left dark:bg-custom_lightblue">
            <button
                type="button"
                className="inline-flex w-full items-center justify-center rounded-md border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50 focus:outline-none dark:border-custom_lightblue dark:bg-custom_lightblue dark:text-gray-200 dark:hover:bg-custom_vista_blue"
                id="options-menu"
                aria-expanded="true"
                aria-haspopup="true"
                onClick={handleToggle}>
                <Icon icon={icon} width="18" height="18" className="mr-2" />
                {label}
            </button>
            {isOpen && (
                <div
                    className="absolute right-0 mt-2 w-40 origin-top-right rounded-md bg-white shadow-lg ring-1 ring-black ring-opacity-5 focus:outline-none dark:bg-custom_space_cadet dark:ring-custom_space_cadet"
                    role="menu"
                    aria-orientation="vertical"
                    aria-labelledby="options-menu">
                    <div className="" role="none">
                        {options.map((option, index) => (
                            <button
                                key={index}
                                className="inline-flex w-full items-center gap-3 px-2.5 py-2.5 text-left text-sm text-gray-700 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-200 dark:hover:bg-custom_vista_blue"
                                role="menuitem"
                                onClick={() => handleSelect(option)}>
                                <Icon icon={option.icon} width="20" height="20" />
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
