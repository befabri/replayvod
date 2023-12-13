import React, { useState, useEffect, useRef, forwardRef, Ref } from "react";
import { Icon } from "@iconify/react";
import { useTranslation } from "react-i18next";
import { NavLink, useLocation } from "react-router-dom";
import { Pathnames } from "../../type/routes";
import { NavLinkBar } from "../../type";

interface SidebarProps extends React.RefAttributes<HTMLDivElement> {
    isOpenSideBar: boolean;
    onCloseSidebar: () => void;
}

const Sidebar: React.FC<SidebarProps> = forwardRef((props: SidebarProps, ref: Ref<HTMLDivElement>) => {
    const { isOpenSideBar, onCloseSidebar } = props;
    const { t } = useTranslation();
    const dropdownRef = useRef<HTMLDivElement | null>(null);
    const location = useLocation();
    const [activeDropdownIndex, setActiveDropdownIndex] = useState<number | null>(null);

    const [navLinks] = useState<NavLinkBar[]>([
        {
            href: Pathnames.Video.Video,
            icon: "mdi:play",
            text: t("Videos"),
            items: [
                { href: Pathnames.Video.Video, text: t("Videos") },
                { href: Pathnames.Video.Channel, text: t("Channels") },
                { href: Pathnames.Video.Category, text: t("Category") },
            ],
        },

        {
            href: "/schedule",
            icon: "mdi:tray-arrow-down",
            text: t("Recording"),
            items: [
                { href: Pathnames.Schedule.Manage, text: t("Manage schedule") },
                { href: Pathnames.Schedule.Following, text: t("Followed Channels") },
            ],
        },
        {
            href: "/activity",
            icon: "fluent:shifts-activity-24-filled",
            text: t("Activity"),
            items: [
                { href: Pathnames.Activity.Queue, text: t("Queue") },
                { href: Pathnames.Activity.History, text: t("History") },
            ],
        },
        {
            href: Pathnames.Settings,
            icon: "mdi:cog",
            text: t("Settings"),
        },
        {
            href: "/system",
            icon: "mdi:laptop",
            text: t("System"),
            items: [
                { href: Pathnames.System.Status, text: t("Status") },
                { href: Pathnames.System.Tasks, text: t("Tasks") },
                { href: Pathnames.System.Events, text: t("Events") },
                { href: Pathnames.System.Logs, text: t("Log Files") },
            ],
        },
    ]);

    useEffect(() => {
        const pageClickEvent = (e: MouseEvent) => {
            if (dropdownRef.current !== null && !dropdownRef.current.contains(e.target as Node)) {
                onCloseSidebar();
            }
        };
        window.addEventListener("click", pageClickEvent);
        return () => {
            window.removeEventListener("click", pageClickEvent);
        };
    }, [onCloseSidebar]);

    useEffect(() => {
        const findActiveDropdownIndex = navLinks.findIndex((link) =>
            link.items?.some((item) => location.pathname.includes(item.href))
        );
        setActiveDropdownIndex(findActiveDropdownIndex >= 0 ? findActiveDropdownIndex : null);
    }, [location.pathname, navLinks]);

    const toggleDropdown = (index: number) => {
        setActiveDropdownIndex((prevIndex) => (prevIndex === index ? null : index));
    };

    return (
        <>
            <aside
                ref={ref}
                id="logo-sidebar"
                className={`fixed top-0 left-0 z-20 w-56 h-screen pt-20 transition-transform ${
                    isOpenSideBar ? "-translate-x-0" : "-translate-x-full"
                } bg-white border-r border-gray-200 md:translate-x-0 dark:bg-custom_lightblue dark:border-custom_lightblue`}
                aria-label="Sidebar">
                <div className="h-full px-3 pb-4 overflow-y-auto bg-white dark:bg-custom_lightblue">
                    <ul className="space-y-2 font-normal">
                        {navLinks.map((link, index) => (
                            <li key={index}>
                                {link.items ? (
                                    <>
                                        <button
                                            onClick={() => toggleDropdown(index)}
                                            className="flex items-center w-full p-2 text-gray-900 transition duration-75 rounded-lg group hover:bg-gray-100 dark:text-white dark:hover:bg-gray-700"
                                            aria-controls="dropdown"
                                            data-collapse-toggle="dropdown">
                                            <Icon icon={link.icon} width="18" height="18" />
                                            <span
                                                className="flex-1 ml-3 text-left whitespace-nowrap"
                                                sidebar-toggle-item="true">
                                                {link.text}
                                            </span>
                                            <Icon icon="mdi:chevron-down" width="18" height="18" />
                                        </button>
                                        {activeDropdownIndex === index && (
                                            <ul id="dropdown" className="py-2 space-y-2">
                                                {link.items.map((item, i) => (
                                                    <li key={i}>
                                                        <NavLink
                                                            to={item.href}
                                                            {...(item.href === Pathnames.Video.Video
                                                                ? { end: true }
                                                                : {})}
                                                            onClick={(e) => e.stopPropagation()}
                                                            className={({ isActive, isPending }) =>
                                                                `flex items-center w-full p-2 text-gray-900 transition duration-75 rounded-lg pl-11 group hover:bg-gray-100 dark:text-white dark:hover:bg-gray-700 ${
                                                                    isPending
                                                                        ? "pending"
                                                                        : isActive
                                                                        ? "dark:bg-gray-700"
                                                                        : "dark:hover:bg-gray-700"
                                                                }`
                                                            }>
                                                            {item.text}
                                                        </NavLink>
                                                    </li>
                                                ))}
                                            </ul>
                                        )}
                                    </>
                                ) : (
                                    <NavLink
                                        to={link.href}
                                        className={({ isActive, isPending }) =>
                                            `flex items-center p-2 text-gray-900 rounded-lg dark:text-white hover:bg-gray-100 ${
                                                isPending
                                                    ? "pending"
                                                    : isActive
                                                    ? "dark:bg-gray-700"
                                                    : "dark:hover:bg-gray-700"
                                            }`
                                        }>
                                        <Icon icon={link.icon} width="18" height="18" />
                                        <span className="ml-3">{link.text}</span>
                                    </NavLink>
                                )}
                            </li>
                        ))}
                    </ul>
                </div>
            </aside>
        </>
    );
});

export default Sidebar;
