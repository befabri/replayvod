import { FC } from "react";

interface ButtonProps {
    text: string;
    typeButton?: "button" | "submit" | "reset";
    disabled?: boolean;
}

const Button: FC<ButtonProps> = ({ text, typeButton = "button", disabled = false }) => {
    return (
        <button
            type={typeButton}
            disabled={disabled}
            className="text-gray-900 bg-white border border-gray-300 focus:outline-none hover:bg-gray-100 focus:ring-0 focus:ring-gray-200 font-medium rounded-lg text-sm px-5 py-2.5 mr-2 mb-2 dark:bg-custom_lightblue dark:text-white dark:border-custom_lightblue dark:hover:bg-custom_vista_blue dark:hover:border-gray-600 dark:focus:ring-custom_lightblue">
            {text}
        </button>
    );
};

export default Button;
