import { FC, MouseEventHandler } from "react";

export interface ButtonProps {
    text?: string;
    onClick?: MouseEventHandler<HTMLButtonElement>;
    typeButton?: "button" | "submit" | "reset";
    disabled?: boolean;
    style?: "primary" | "inverted" | "submit" | "svg";
    children?: React.ReactNode;
}

const styles = {
    primary:
        " flex items-center px-6 py-2 text-white bg-custom_lightblue rounded-md hover:bg-custom_vista_blue font-medium",
    inverted:
        " flex items-center px-6 py-2 text-white bg-custom_vista_blue rounded-md hover hover:bg-custom_lightblue font-medium",
    submit: " flex items-center px-6 py-2 text-white bg-custom_delft_blue rounded-md hover:bg-custom_vista_blue font-medium",
    svg: " flex items-center focus:outline-none hover:bg-gray-100 focus:ring-gray-200 font-medium rounded-lg text-sm px-2 py-2.5  dark:text-white  dark:hover:bg-custom_vista_blue dark:focus:ring-gray-700",
};

const getButtonStyle = (styleType: "primary" | "inverted" | "submit" | "svg") => {
    return styles[styleType] || styles.primary;
};

const Button: FC<ButtonProps> = ({
    text = "",
    onClick,
    children,
    typeButton = "button",
    disabled = false,
    style = "primary",
}) => {
    return (
        <button type={typeButton} onClick={onClick} disabled={disabled} className={getButtonStyle(style)}>
            {children}
            {text}
        </button>
    );
};

export default Button;
