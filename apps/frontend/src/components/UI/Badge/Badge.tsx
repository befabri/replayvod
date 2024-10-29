import React from "react";

type BadgeColor = "gray" | "red" | "yellow" | "green" | "blue" | "indigo" | "purple" | "pink" | "orange" | "teal";

interface BadgeProps {
    children: React.ReactNode;
    color: BadgeColor;
    className?: string;
}
const colorStyles = {
    gray: "bg-gray-900 text-gray-300",
    red: "bg-red-900 text-red-300",
    yellow: "bg-yellow-900 text-yellow-300",
    green: "bg-green-900 text-green-300",
    blue: "bg-blue-900 text-blue-300",
    indigo: "bg-indigo-900 text-indigo-300",
    purple: "bg-purple-900 text-purple-300",
    pink: "bg-pink-900 text-pink-300",
    orange: "bg-orange-900 text-orange-300",
    teal: "bg-teal-900 text-teal-300",
};

const Badge: React.FC<BadgeProps> = ({ children, color, className = "" }) => {
    return (
        <span
            className={`inline-flex select-none items-center rounded px-2.5 py-0.5 text-sm font-semibold ${colorStyles[color]} ${className}`}>
            {children}
        </span>
    );
};

export default Badge;
