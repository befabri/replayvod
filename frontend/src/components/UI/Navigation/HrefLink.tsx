import React from "react";
import { Link } from "react-router-dom";

interface HrefLinkProps {
    to: string;
    children: React.ReactNode;
    style?: "title" | "text" | "normal";
}

const styles = {
    title: "text-base font-semibold dark:text-gray-100 inline",
    text: "text-sm text-gray-500 dark:text-gray-400 inline",
    normal: "text-black dark:text-white inline",
};

const getHrefLinkStyle = (styleType: "title" | "text" | "normal") => {
    return `${styles[styleType]} hover:text-custom_vista_blue dark:hover:text-custom_vista_blue`;
};

const HrefLink: React.FC<HrefLinkProps> = ({ to, children, style = "text" }) => {
    return (
        <Link to={to} className={getHrefLinkStyle(style)}>
            {children}
        </Link>
    );
};

export default HrefLink;
