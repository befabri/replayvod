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
            disabled={disabled}
            required={required}
            min={minValue}
            className={`block w-full rounded-lg border border-gray-300 bg-gray-50 p-2.5 text-sm text-gray-900 focus:border-custom_lightblue focus:ring-custom_lightblue dark:border-custom_lightblue dark:bg-custom_lightblue dark:text-white dark:placeholder-gray-400 dark:focus:border-custom_lightblue dark:focus:ring-custom_lightblue disabled:dark:bg-custom_blue`}
        />
        {error && <p className=" self-start rounded-md px-2 py-1 italic text-red-500">{error?.message}</p>}
    </>
);

export default InputNumber;
