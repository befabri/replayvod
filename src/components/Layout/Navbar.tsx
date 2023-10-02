import React, { useState, useEffect, useRef } from "react";
import DarkModeToggle from "../UI/Toggle/DarkModeToggle";
import Sidebar from "./Sidebar";
import { Icon } from "@iconify/react";
import { useAuth } from "../../pages/Auth";
import i18n from "i18next";

const Navbar: React.FC = () => {
    const [isSidebarOpen, setSidebarOpen] = useState(false);
    const [isProfilOpen, setProfileOpen] = useState(false);
    const [isLanguageOpen, setLanguageOpen] = useState(false);
    const navbarRef = useRef<HTMLDivElement | null>(null);
    const sidebarRef = useRef<HTMLDivElement | null>(null);
    const { user, signOut } = useAuth();

    const languages = ["English", "Français"];

    const languageMap = {
        English: "en",
        Français: "fr",
    };

    const handleSidebarOpen = () => {
        if (isProfilOpen) {
            setProfileOpen(false);
        }
        if (isLanguageOpen) {
            setLanguageOpen(false);
        }
        setSidebarOpen(!isSidebarOpen);
    };

    const handleSidebarClose = () => {
        setSidebarOpen(false);
    };

    const handleProfileToggle = () => {
        if (isLanguageOpen) {
            setLanguageOpen(false);
        }
        setProfileOpen(!isProfilOpen);
    };

    const handleSelect = (event: React.MouseEvent, option: string) => {
        event.preventDefault();
        if (option === "Sign Out") {
            signOut();
        }
        if (option === "Language") {
            setProfileOpen(false);
            setLanguageOpen(true);
        }
    };

    const handleLanguage = (event: React.MouseEvent, language: string) => {
        event.preventDefault();
        const langCode = languageMap[language as keyof typeof languageMap];
        if (langCode) {
            i18n.changeLanguage(langCode);
            setLanguageOpen(false);
        }
    };

    useEffect(() => {
        const handleOutsideClick = (event: MouseEvent) => {
            if (sidebarRef.current && !sidebarRef.current.contains(event.target as Node)) {
                setSidebarOpen(false);
            }
        };

        document.addEventListener("mouseup", handleOutsideClick);

        return () => {
            document.removeEventListener("mouseup", handleOutsideClick);
        };
    }, []);

    useEffect(() => {
        const closeDropdowns = (event: MouseEvent) => {
            const target = event.target as Node;
            if (
                (isProfilOpen && !document.getElementById("dropdown-user")?.contains(target)) ||
                (isLanguageOpen && !document.getElementById("dropdown-language")?.contains(target))
            ) {
                setProfileOpen(false);
                setLanguageOpen(false);
            }
        };

        document.addEventListener("mouseup", closeDropdowns);
        return () => {
            document.removeEventListener("mouseup", closeDropdowns);
        };
    }, [isProfilOpen, isLanguageOpen]);

    return (
        <>
            <nav
                ref={navbarRef}
                className="fixed top-0 z-50 w-full bg-white border-b border-gray-200 dark:bg-gray-800 dark:border-gray-700">
                <div className="px-3 py-3 lg:px-5 lg:pl-3">
                    <div className="flex items-center justify-between">
                        <div className="flex items-center justify-start">
                            <button
                                onClick={handleSidebarOpen}
                                type="button"
                                className="inline-flex items-center p-2 text-sm text-gray-500 rounded-lg md:hidden hover:bg-gray-100 focus:outline-none focus:ring-2 focus:ring-gray-200 dark:text-gray-400 dark:hover:bg-gray-700 dark:focus:ring-gray-600">
                                <span className="sr-only">Open sidebar</span>
                                <svg
                                    className="w-6 h-6"
                                    aria-hidden="true"
                                    fill="currentColor"
                                    viewBox="0 0 20 20"
                                    xmlns="http://www.w3.org/2000/svg">
                                    <path d="M2 4.75A.75.75 0 012.75 4h14.5a.75.75 0 010 1.5H2.75A.75.75 0 012 4.75zm0 10.5a.75.75 0 01.75-.75h7.5a.75.75 0 010 1.5h-7.5a.75.75 0 01-.75-.75zM2 10a.75.75 0 01.75-.75h14.5a.75.75 0 010 1.5H2.75A.75.75 0 012 10z"></path>
                                </svg>
                            </button>
                            <a href="/" className="flex ml-2 md:mr-24">
                                <span className="self-center text-xl font-semibold md:text-2xl whitespace-nowrap dark:text-white">
                                    Rediff
                                </span>
                            </a>
                        </div>
                        <div className="flex items-center">
                            <div className="flex items-center ml-3">
                                <div>
                                    <button
                                        onClick={handleProfileToggle}
                                        type="button"
                                        className="flex text-sm bg-gray-800 rounded-full focus:ring-4 focus:ring-gray-300 dark:focus:ring-gray-600"
                                        aria-expanded="false"
                                        data-dropdown-toggle="dropdown-user">
                                        <span className="sr-only">Open user menu</span>
                                        <img
                                            className="w-8 h-8 rounded-full"
                                            src={user ? user.profile_image : "/images/placeholder_picture.png"}
                                            alt="user photo"
                                        />
                                    </button>
                                </div>
                                {isProfilOpen && (
                                    <div
                                        className="origin-top-right absolute right-5 top-11 z-60 mt-2 w-40 rounded-md shadow-lg bg-white ring-1 ring-black ring-opacity-5 focus:outline-none dark:bg-gray-700 dark:ring-gray-600 divide-y divide-gray-100 dark:divide-gray-600"
                                        id="dropdown-user">
                                        <div>
                                            <a
                                                href="#"
                                                className="flex gap-9 items-center px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-200 dark:hover:bg-gray-600"
                                                role="menuitem"
                                                onClick={(e: React.MouseEvent) => handleSelect(e, "Language")}>
                                                <span>Language</span>
                                                <Icon icon="mdi:chevron-right" width="18" height="18" />
                                            </a>
                                            <DarkModeToggle className="h-9 block px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-200 dark:hover:bg-gray-600" />
                                        </div>
                                        <a
                                            href="#"
                                            className="block px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-200 dark:hover:bg-gray-600"
                                            role="menuitem"
                                            onClick={(e: React.MouseEvent) => handleSelect(e, "Sign Out")}>
                                            Sign Out
                                        </a>
                                    </div>
                                )}
                                {isLanguageOpen && (
                                    <div
                                        id="dropdown-language"
                                        className="origin-top-right absolute right-5 top-11 z-60 mt-2 w-40 rounded-md shadow-lg bg-white ring-1 ring-black ring-opacity-5 focus:outline-none dark:bg-gray-700 dark:ring-gray-600 divide-y divide-gray-100 dark:divide-gray-600">
                                        <div
                                            className="cursor-pointer flex items-center justify-between w-full px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-200 dark:hover:bg-gray-600"
                                            onClick={() => {
                                                setLanguageOpen(false);
                                                setProfileOpen(true);
                                            }}>
                                            <Icon icon="mdi:chevron-left" width="18" height="18" />
                                            <span>Languages</span>
                                            <div></div>
                                        </div>

                                        <div className="divide-y divide-gray-100 dark:divide-gray-600">
                                            {languages.map((lang) => (
                                                <a
                                                    key={lang}
                                                    href="#"
                                                    className="block px-4 py-2 text-sm text-gray-700 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-200 dark:hover:bg-gray-600"
                                                    role="menuitem"
                                                    onClick={(e: React.MouseEvent) => handleLanguage(e, lang)}>
                                                    {lang}
                                                </a>
                                            ))}
                                        </div>
                                    </div>
                                )}
                            </div>
                        </div>
                    </div>
                </div>
            </nav>
            {isSidebarOpen && (
                <Sidebar ref={sidebarRef} isOpenSideBar={isSidebarOpen} onCloseSidebar={handleSidebarClose} />
            )}
        </>
    );
};

export default Navbar;
