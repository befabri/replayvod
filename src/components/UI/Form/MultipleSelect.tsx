import { FC, useRef, useState } from "react";
import useOutsideClick from "../../../hooks/useOutsideClick";
import classNames from "classnames";

interface SelectProps {
    label?: string;
    id: string;
    error: any;
    options: string[];
    disabled?: boolean;
    onCategoriesChange: any;
    value?: string[];
}

const MultipleSelect: FC<SelectProps> = ({
    label,
    id,
    error,
    options,
    onCategoriesChange,
    disabled = false,
    value,
}) => {
    const [categories, setCategories] = useState<string[]>(value || []);
    const [showPopup, setShowPopup] = useState<boolean>(false);
    const popupRef = useRef(null);

    useOutsideClick(popupRef, () => setShowPopup(false));

    const togglePopup = () => {
        setShowPopup(!showPopup);
    };

    const handleAddCategory = (category: string) => {
        if (!categories.includes(category)) {
            setCategories([...categories, category]);
            onCategoriesChange([...categories, category]);
        }
        togglePopup();
    };

    const handleRemove = (item: string) => {
        setCategories((curr) => {
            const newTags = curr.filter((cat) => cat !== item);
            onCategoriesChange(newTags);
            return newTags;
        });
    };

    return (
        <>
            {label && (
                <label className="mb-2 block text-sm font-medium text-gray-900 dark:text-white" htmlFor={id}>
                    {label}
                </label>
            )}

            <div className="mx-auto flex w-full flex-col ">
                <div className="w-full">
                    <div ref={popupRef} className="relative flex flex-col items-center">
                        <div className="w-full">
                            <div
                                className={classNames(
                                    `flex h-12 w-full flex-wrap items-center rounded-lg border bg-gray-50 px-2.5 text-sm text-gray-900 focus:border-blue-500 focus:ring-blue-500 dark:border-custom_lightblue dark:text-white dark:placeholder-gray-400 dark:focus:border-custom_lightblue dark:focus:ring-custom_vista_blue`,
                                    {
                                        "dark:bg-custom_blue": disabled,
                                        "dark:bg-custom_lightblue": !disabled,
                                    }
                                )}>
                                {categories.map((category) => (
                                    <span
                                        key={category}
                                        className="me-2 inline-flex items-center rounded-full bg-blue-100 px-3 py-1 text-sm font-medium text-blue-800 dark:bg-blue-900 dark:text-blue-200">
                                        {category}
                                        <button
                                            type="button"
                                            onClick={() => handleRemove(category)}
                                            className="ms-2 inline-flex items-center bg-transparent text-sm text-blue-400 hover:bg-blue-200 hover:text-blue-900 dark:hover:bg-blue-800 dark:hover:text-blue-200"
                                            data-dismiss-target="#badge-dismiss-default"
                                            aria-label="Remove">
                                            <svg
                                                className="h-2 w-2"
                                                aria-hidden="true"
                                                xmlns="http://www.w3.org/2000/svg"
                                                fill="none"
                                                viewBox="0 0 14 14">
                                                <path
                                                    stroke="currentColor"
                                                    strokeLinecap="round"
                                                    strokeLinejoin="round"
                                                    strokeWidth="2"
                                                    d="m1 1 6 6m0 0 6 6M7 7l6-6M7 7l-6 6"
                                                />
                                            </svg>
                                            <span className="sr-only">Remove badge</span>
                                        </button>
                                    </span>
                                ))}
                                <div className="h-full flex-1">
                                    <input
                                        onClick={() => togglePopup()}
                                        placeholder=""
                                        disabled={disabled}
                                        className="h-full w-full flex-1 rounded-lg border-0 border-none border-transparent bg-gray-50 p-0 text-sm text-gray-900 focus-within:border-0 focus-within:border-transparent focus-within:outline-none focus:border-none focus:ring-0 dark:bg-custom_lightblue dark:text-white dark:placeholder-gray-400 disabled:dark:bg-custom_blue"
                                    />
                                </div>
                            </div>
                        </div>
                        {showPopup && (
                            <div className="z-80 absolute left-0 top-14 max-h-64 w-full overflow-y-auto rounded border border-custom_space_cadet_bis bg-white dark:bg-custom_lightblue">
                                <div className="flex w-full flex-col">
                                    {options.map((cat) => (
                                        <div
                                            key={cat}
                                            className="w-full cursor-pointer border-b border-gray-100 dark:border-custom_lightblue"
                                            onClick={() => handleAddCategory(cat)}>
                                            <div className="relative flex w-full items-center border-l-2 border-transparent p-2 pl-2 hover:bg-custom_vista_blue">
                                                <div className="flex w-full items-center">
                                                    <div className="mx-2 leading-6 dark:text-white">{cat}</div>
                                                </div>
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            </div>
                        )}
                    </div>
                </div>
            </div>
            {error && (
                <span className=" self-start rounded-md px-2 py-1 italic text-red-500">{error.message}</span>
            )}
        </>
    );
};

export default MultipleSelect;
