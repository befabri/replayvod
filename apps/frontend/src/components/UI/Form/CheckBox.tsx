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
            <div className="flex h-5 items-center">
                <input
                    id={id}
                    type="checkbox"
                    {...register}
                    className="h-4 w-4 rounded border-gray-300 bg-gray-100 text-blue-600 focus:ring-0 focus:ring-custom_lightblue dark:border-custom_lightblue dark:bg-custom_lightblue dark:ring-offset-custom_lightblue dark:hover:border-custom_vista_blue dark:hover:bg-custom_lightblue dark:focus:bg-custom_lightblue dark:focus:ring-0 "
                />
            </div>
            <div className="mb-2 ml-2 text-sm">
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
