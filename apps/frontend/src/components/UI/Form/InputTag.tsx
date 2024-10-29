import { FC, SetStateAction, useRef, useState } from "react";
import classNames from "classnames";

interface InputTagProps {
    label?: string;
    id: string;
    error?: any;
    disabled?: boolean;
    onTagsChange: any;
    value?: string[];
}

const InputTag: FC<InputTagProps> = ({ label, id, error, disabled = false, onTagsChange, value }) => {
    const [tags, setTags] = useState<string[]>(value || []);
    const [inputValue, setInputValue] = useState("");
    const inputRef = useRef<HTMLInputElement>(null);

    const updateTags = (newTags: string[]) => {
        setTags(newTags);
        onTagsChange(newTags);
    };

    const handleKeyDown = (e: { key: string; preventDefault: () => void }) => {
        if (e.key === "Enter" && inputValue.trim() !== "") {
            e.preventDefault();
            updateTags([...tags, inputValue.trim()]);
            setInputValue("");
        }
    };

    const handleChange = (e: { target: { value: SetStateAction<string> } }) => {
        setInputValue(e.target.value);
    };

    const handleRemove = (indexToRemove: number) => {
        setTags((currentTags) => {
            const newTags = currentTags.filter((_, index) => index !== indexToRemove);
            onTagsChange(newTags);
            return newTags;
        });
    };

    const focusInput = () => {
        if (inputRef.current) {
            inputRef.current.focus();
        }
    };

    return (
        <>
            {label && (
                <label className="mb-2 block text-sm font-medium text-gray-900 dark:text-white" htmlFor={id}>
                    {label}
                </label>
            )}

            <div
                className={classNames(
                    `flex h-12 w-full flex-wrap items-center rounded-lg border bg-gray-50 px-2.5 text-sm text-gray-900 focus:border-blue-500 focus:ring-blue-500 dark:border-custom_lightblue dark:text-white dark:placeholder-gray-400 dark:focus:border-custom_lightblue dark:focus:ring-custom_vista_blue`,
                    {
                        "dark:bg-custom_blue": disabled,
                        "dark:bg-custom_lightblue": !disabled,
                    }
                )}
                onClick={focusInput}>
                {tags.map((tag, index) => (
                    <span
                        key={index}
                        id="badge-dismiss-default"
                        className="me-2 inline-flex items-center rounded-full bg-emerald-100 px-3 py-1 text-sm font-medium text-emerald-800 dark:bg-emerald-900 dark:text-emerald-200">
                        {tag}
                        <button
                            type="button"
                            onClick={() => handleRemove(index)}
                            className="ms-2 inline-flex items-center bg-transparent text-sm text-emerald-400 hover:bg-emerald-200 hover:text-emerald-900 dark:hover:bg-emerald-800 dark:hover:text-emerald-200"
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
                <input
                    type="text"
                    id={id}
                    value={inputValue}
                    onChange={handleChange}
                    onKeyDown={handleKeyDown}
                    disabled={disabled}
                    className={classNames(
                        ` h-full flex-1 rounded-lg border-0 border-none border-transparent bg-gray-50 p-0 text-sm text-gray-900 focus-within:border-0 focus-within:border-transparent focus-within:outline-none focus:border-none  focus:ring-0 dark:bg-custom_lightblue dark:text-white  dark:placeholder-gray-400 disabled:dark:bg-custom_blue`
                    )}
                />
            </div>

            {error && <p className="mt-1 text-xs italic text-red-500">{error?.message}</p>}
        </>
    );
};

export default InputTag;
