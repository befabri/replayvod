import { FC } from "react";

interface SelectProps {
    label?: string;
    id: string;
    register: any;
    error: any;
    options: string[];
    required?: boolean;
    disabled?: boolean;
}

const Select: FC<SelectProps> = ({ label, id, register, error, options, required, disabled = false }) => {
    return (
        <>
            {label && (
                <label className="block mb-2 text-sm font-medium text-gray-900 dark:text-white" htmlFor={id}>
                    {label}
                </label>
            )}

            <select
                {...register}
                id={id}
                required={required}
                disabled={disabled}
                className={`bg-gray-50 border border-gray-300 text-gray-900 text-sm rounded-lg focus:ring-custom_vista_blue focus:border-custom_vista_blue block w-full p-2.5  dark:border-custom_lightblue dark:placeholder-gray-400 dark:text-white dark:focus:ring-custom_vista_blue dark:focus:border-custom_vista_blue dark:hover:border-custom_vista_blue
                ${disabled ? "dark:bg-custom_black" : "dark:bg-custom_lightblue"}`}>
                {options.map((option) => (
                    <option key={option} value={option}>
                        {option}
                    </option>
                ))}
            </select>
            {error && (
                <span className=" text-red-500 italic px-2 py-1 rounded-md self-start">{error.message}</span>
            )}
        </>
    );
};

export default Select;
