import { FC, ChangeEvent, FocusEventHandler } from "react";
import classNames from "classnames";

interface InputTextProps {
  label: string;
  id: string;
  value: string;
  onChange: (e: ChangeEvent<HTMLInputElement>) => void;
  placeholder: string;
  required?: boolean;
  list?: string;
  onBlur?: FocusEventHandler<HTMLInputElement>;
  isValid?: boolean;
}

const InputText: FC<InputTextProps> = ({
  label,
  id,
  value,
  onChange,
  placeholder,
  required,
  list,
  onBlur,
  isValid = true,
}) => (
  <div>
    <label htmlFor={id} className="block mb-2 text-sm font-medium text-gray-900 dark:text-white">
      {label}
    </label>
    <input
      type="text"
      id={id}
      value={value}
      onChange={onChange}
      className={classNames(
        "bg-gray-50 border text-gray-900 text-sm rounded-lg focus:ring-blue-500 focus:border-blue-500 block w-full p-2.5 dark:bg-gray-700  dark:placeholder-gray-400 dark:text-white dark:focus:ring-blue-500 dark:focus:border-blue-500",
        {
          "dark:border-red-600 border-red-600": !isValid,
          "dark:border-gray-600": isValid,
        }
      )}
      placeholder={placeholder}
      required={required}
      list={list}
      onBlur={onBlur}
    />
  </div>
);

export default InputText;
