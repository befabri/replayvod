import React, { createContext, useContext, useState, useEffect } from "react";

const DarkModeContext = createContext<{
    isDarkMode: boolean;
    toggleDarkMode: () => void;
}>({
    isDarkMode: false,
    toggleDarkMode: () => {},
});

export const DarkModeProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
    const [isDarkMode, setIsDarkMode] = useState(() => {
        const storedMode = localStorage.getItem("darkMode");
        return storedMode !== null ? JSON.parse(storedMode) : false;
    });

    useEffect(() => {
        localStorage.setItem("darkMode", JSON.stringify(isDarkMode));
        if (isDarkMode) {
            document.body.classList.add("dark");
        } else {
            document.body.classList.remove("dark");
        }
    }, [isDarkMode]);
    const toggleDarkMode = () => setIsDarkMode((prevMode: boolean) => !prevMode);

    return <DarkModeContext.Provider value={{ isDarkMode, toggleDarkMode }}>{children}</DarkModeContext.Provider>;
};

export const useDarkMode = () => {
    const context = useContext(DarkModeContext);
    if (!context) {
        throw new Error("useDarkMode must be used within a DarkModeProvider");
    }
    return context;
};
