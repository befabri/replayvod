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
            className={`bg-gray-50 border border-gray-300 text-gray-900 text-sm rounded-lg focus:ring-custom_lightblue focus:border-custom_lightblue block w-full p-2.5  dark:border-custom_lightblue dark:placeholder-gray-400 dark:text-white dark:focus:ring-custom_lightblue dark:focus:border-custom_lightblue
            ${disabled ? "dark:bg-custom_black" : "dark:bg-custom_lightblue"}`}
        />
        {error && <p className=" text-red-500 italic px-2 py-1 rounded-md self-start">{error?.message}</p>}
    </>
);

export default InputNumber;
