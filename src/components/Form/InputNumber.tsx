import { FC } from "react";

interface InputNumberProps {
    id: string;
    required?: boolean;
    register?: any;
    error?: any;
    disabled?: boolean;
    minValue: number;
}

const InputNumber: FC<InputNumberProps> = ({ id, required, register, error, disabled, minValue }) => (
    <>
        <input
            {...register}
            type="number"
            id={id}
            required={required}
            min={minValue}
            className={`bg-gray-50 border border-gray-300 text-gray-900 text-sm rounded-lg focus:ring-blue-500 focus:border-blue-500 block w-full p-2.5  dark:border-gray-600 dark:placeholder-gray-400 dark:text-white dark:focus:ring-blue-500 dark:focus:border-blue-500
            ${disabled ? "dark:bg-gray-800" : "dark:bg-gray-700"}`}
        />
        {error && <p className=" text-red-500 italic px-2 py-1 rounded-md self-start">{error?.message}</p>}
    </>
);

export default InputNumber;
