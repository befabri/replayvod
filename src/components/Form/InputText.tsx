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
            <label className="block mb-2 text-sm font-medium text-gray-900 dark:text-white" htmlFor={id}>
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
                `${
                    disabled ? "dark:bg-custom_black" : "dark:bg-custom_lightblue"
                } bg-gray-50 border text-gray-900 text-sm rounded-lg focus:ring-blue-500 focus:border-blue-500 block w-full p-2.5 dark:placeholder-gray-400 dark:text-white dark:focus:ring-custom_vista_blue dark:focus:border-custom_lightblue`,
                {
                    "dark:border-red-600 border-red-600": error,
                    "dark:border-custom_lightblue": !error,
                }
            )}
        />
        {error && <p className=" text-red-500 italic px-2 py-1 rounded-md self-start">{error?.message}</p>}
    </>
);

export default InputText;
