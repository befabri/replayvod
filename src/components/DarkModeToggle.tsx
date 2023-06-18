import { useEffect, useState } from "react";
import { Icon } from "@iconify/react";

const DarkModeToggle = () => {
  const [isDarkMode, setIsDarkMode] = useState(() => {
    const storedMode = window.localStorage.getItem("darkMode");
    return storedMode !== null ? JSON.parse(storedMode) : document.documentElement.classList.contains("dark");
  });

  useEffect(() => {
    window.localStorage.setItem("darkMode", JSON.stringify(isDarkMode));
    const classList = document.documentElement.classList;
    const className = "dark";
    isDarkMode ? classList.add(className) : classList.remove(className);
  }, [isDarkMode]);

  const handleToggle = () => {
    setIsDarkMode((prevMode: any) => !prevMode);
  };

  return (
    <button onClick={handleToggle} className="flex items-center justify-end focus:outline-none">
      {isDarkMode ? (
        <Icon icon="iconoir:sun-light" className="w-6 h-6 text-gray-500 dark:text-gray-300" />
      ) : (
        <Icon icon="iconoir:half-moon" className="w-6 h-6 text-gray-500 dark:text-gray-300" />
      )}
    </button>
  );
};

export default DarkModeToggle;
