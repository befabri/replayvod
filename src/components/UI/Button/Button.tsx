import { FC, MouseEventHandler } from "react";

interface ButtonProps {
    text?: string;
    onClick: MouseEventHandler<HTMLButtonElement>;
    disabled?: boolean;
    children?: any;
    style?: "primary" | "svg";
}

const styles = {
    primary:
        " bg-white border border-gray-300 focus:outline-none hover:bg-gray-100 focus:ring-4 focus:ring-gray-200 font-medium rounded-lg text-sm px-5 py-2.5 mr-2 mb-2 dark:bg-gray-800 dark:text-white dark:border-gray-600 dark:hover:bg-gray-700 dark:hover:border-gray-600 dark:focus:ring-gray-700",
    svg: "focus:outline-none hover:bg-gray-100 focus:ring-gray-200 font-medium rounded-lg text-sm px-2 py-2.5  dark:text-white  dark:hover:bg-gray-700  dark:focus:ring-gray-700",
};

const getButtonStyle = (styleType: "primary" | "svg") => {
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
