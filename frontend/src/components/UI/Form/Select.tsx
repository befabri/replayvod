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
                <label className="mb-2 block text-sm font-medium text-gray-900 dark:text-white" htmlFor={id}>
                    {label}
                </label>
            )}

            <select
                {...register}
                id={id}
                required={required}
                disabled={disabled}
                className={`block w-full rounded-lg border border-gray-300 bg-gray-50 p-2.5 text-sm text-gray-900 opacity-100 focus:border-custom_vista_blue focus:ring-custom_vista_blue dark:border-custom_lightblue dark:bg-custom_lightblue  dark:text-white dark:placeholder-gray-400 dark:hover:border-custom_vista_blue dark:focus:border-custom_vista_blue dark:focus:ring-custom_vista_blue disabled:dark:bg-custom_blue`}>
                {options.map((option) => (
                    <option key={option} value={option}>
                        {option}
                    </option>
                ))}
            </select>
            {error && (
                <span className=" self-start rounded-md px-2 py-1 italic text-red-500">{error.message}</span>
            )}
        </>
    );
};

export default Select;
