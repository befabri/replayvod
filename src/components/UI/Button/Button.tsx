import { FC, MouseEventHandler } from "react";

export interface ButtonProps {
    text?: string;
    onClick: MouseEventHandler<HTMLButtonElement>;
    disabled?: boolean;
    children?: React.ReactNode;
    style?: "primary" | "svg" | "inverted" | "old";
}

const styles = {
    primary:
        " flex items-center px-6 py-2 text-white bg-custom_lightblue rounded-md hover:bg-custom_vista_blue font-medium ",
    inverted:
        " flex items-center px-6 py-2 text-white bg-custom_vista_blue rounded-md hover hover:bg-custom_lightblue font-medium ",
    old: " flex items-center bg-white border border-gray-300 focus:outline-none hover:bg-gray-100 focus:ring-4 focus:ring-gray-200 font-medium rounded-lg text-sm px-5 py-2.5 mr-2 mb-2 dark:bg-gray-800 dark:text-white dark:border-gray-600 dark:hover:bg-gray-700 dark:hover:border-gray-600 dark:focus:ring-gray-700",
    svg: " flex items-center focus:outline-none hover:bg-gray-100 focus:ring-gray-200 font-medium rounded-lg text-sm px-2 py-2.5  dark:text-white  dark:hover:bg-gray-700  dark:focus:ring-gray-700",
};

const getButtonStyle = (styleType: "primary" | "inverted" | "svg" | "old") => {
    return `${styles[styleType]} text-gray-900`;
};

const Button: FC<ButtonProps> = ({ text = "", onClick, disabled = false, children, style = "primary" }) => {
    return (
        <button type="button" onClick={onClick} disabled={disabled} className={getButtonStyle(style)}>
            {children}
            {text}
        </button>
    );
};

export default Button;
