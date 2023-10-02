import React from "react";

interface ToggleSwitchProps {
    label: string;
    register: any;
    id: string;
    error?: any;
}

const ToggleSwitch: React.FC<ToggleSwitchProps> = ({ label, register, id, error }) => {
    return (
        <>
            <label className="block mb-2 text-sm font-medium text-gray-900 dark:text-white" htmlFor={id}>
                {label}
            </label>
            <label className="relative inline-flex items-center mb-4 cursor-pointer">
                <input type="checkbox" {...register} id={id} className="sr-only peer" />
                <div className="w-11 h-6 bg-gray-200 rounded-full peer peer-focus:ring-4 peer-focus:ring-blue-300 dark:peer-focus:ring-blue-800 dark:bg-gray-700 peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-0.5 after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all dark:border-gray-600 peer-checked:bg-blue-600"></div>
            </label>
            {error && (
                <span className=" text-red-500 italic px-2 py-1 rounded-md self-start">{error?.message}</span>
            )}
        </>
    );
};

export default ToggleSwitch;
