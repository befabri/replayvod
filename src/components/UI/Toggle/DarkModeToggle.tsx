import { useDarkMode } from "../../../context/Themes/DarkModeContext";

const DarkModeToggle = ({ className, text }: { className?: string; text: string }) => {
    const { isDarkMode, toggleDarkMode } = useDarkMode();

    return (
        <div className={`flex justify-between items-center ${className}`} onClick={toggleDarkMode}>
            <span className="h-6 mr-2 cursor-pointer">{text}</span>
            <button className="flex items-center justify-end focus:outline-none cursor-pointer">
                <div className="transform scale-75">
                    <label className="relative inline-flex items-center cursor-pointer">
                        <input
                            type="checkbox"
                            value=""
                            className="sr-only peer"
                            checked={isDarkMode}
                            onChange={toggleDarkMode}
                        />
                        <div className="w-11 h-6 bg-gray-200 peer-focus:outline-none peer-focus:ring-4 peer-focus:ring-blue-300 dark:peer-focus:ring-blue-800 rounded-full peer dark:bg-gray-700 peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all dark:border-gray-600 peer-checked:bg-blue-600"></div>
                    </label>
                </div>
            </button>
        </div>
    );
};

export default DarkModeToggle;
