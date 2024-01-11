import React from "react";

interface CheckBoxProps {
    label: string;
    register: any;
    id: string;
    error?: any;
    helperText?: string;
}

const CheckBox: React.FC<CheckBoxProps> = ({ label, register, id, error, helperText }) => {
    return (
        <div className="flex">
            <div className="flex items-center h-5">
                <input
                    id={id}
                    type="checkbox"
                    {...register}
                    className="w-4 h-4 text-blue-600 dark:hover:bg-custom_lightblue dark:hover:border-custom_vista_blue bg-gray-100 border-gray-300 rounded focus:ring-custom_lightblue dark:focus:bg-custom_lightblue dark:ring-offset-custom_lightblue focus:ring-0 dark:focus:ring-0 dark:bg-custom_lightblue dark:border-custom_lightblue "
                />
            </div>
            <div className="ml-2 text-sm  mb-2">
                <label htmlFor={id} className="font-medium text-gray-900 dark:text-gray-300">
                    {label}
                </label>
                {helperText && (
                    <p id="helper-checkbox-text" className="text-xs font-normal text-gray-500 dark:text-gray-300">
                        {helperText}
                    </p>
                )}
                {error && <p className="text-xs font-normal text-red-500">{error?.message}</p>}
            </div>
        </div>
    );
};

export default CheckBox;
