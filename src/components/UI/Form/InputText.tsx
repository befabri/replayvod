import { FC } from "react";
import classNames from "classnames";

interface InputTextProps {
    label?: string;
    id: string;
    placeholder: string;
    required?: boolean;
    list?: string;
    register?: any;
    error?: any;
    onBlur?: () => void;
    onChange?: any;
    disabled?: boolean;
}

const InputText: FC<InputTextProps> = ({
    label,
    id,
    placeholder,
    required,
    list,
    register,
    error,
    onBlur,
    onChange,
    disabled = false,
}) => (
    <>
        {label && (
            <label className="mb-2 block text-sm font-medium text-gray-900 dark:text-white" htmlFor={id}>
                {label}
            </label>
        )}

        <input
            {...register}
            type="text"
            id={id}
            placeholder={placeholder}
            required={required}
            onBlur={onBlur}
            onChange={(e) => {
                register.onChange(e);
                if (onChange) onChange(e);
            }}
            list={list}
            disabled={disabled}
            className={classNames(
                ` block w-full rounded-lg border bg-gray-50 p-2.5 text-sm text-gray-900 focus:border-blue-500 focus:ring-blue-500 dark:bg-custom_lightblue dark:text-white dark:placeholder-gray-400 dark:focus:border-custom_lightblue dark:focus:ring-custom_vista_blue disabled:dark:bg-custom_blue`,
                {
                    "border-red-600 dark:border-red-600": error,
                    "dark:border-custom_lightblue": !error,
                }
            )}
        />
        {error && <p className=" self-start rounded-md px-2 py-1 italic text-red-500">{error?.message}</p>}
    </>
);

export default InputText;
