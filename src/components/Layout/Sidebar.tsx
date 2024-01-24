import React, { useState, useEffect, useRef, forwardRef, Ref } from "react";
import { Icon } from "@iconify/react";
import { useTranslation } from "react-i18next";
import { NavLink, useLocation } from "react-router-dom";
import { Pathnames } from "../../type/routes";
import { NavLinkBar } from "../../type";
import useOutsideClick from "../../hooks/useOutsideClick";

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

    useOutsideClick(dropdownRef, () => onCloseSidebar());

    const [navLinks] = useState<NavLinkBar[]>([
        {
            href: Pathnames.Home,
            icon: "mdi:home",
            text: t("Dashboard"),
        },
        {
            href: Pathnames.Video.Video,
            icon: "mdi:play",
            text: t("Videos"),
            items: [
                { href: Pathnames.Video.Video, text: t("Videos") },
                { href: Pathnames.Video.Category, text: t("Category") },
                { href: Pathnames.Video.Channel, text: t("Channels") },
            ],
        },

        {
            href: "/schedule",
            icon: "mdi:tray-arrow-down",
            text: t("Recording"),
            items: [
                { href: Pathnames.Schedule.Add, text: t("Schedule") },
                { href: Pathnames.Schedule.EventSub, text: t("Event sub") },
                { href: Pathnames.Schedule.Manage, text: t("Manage schedule") },
            ],
        },
        {
            href: "/activity",
            icon: "mdi:list-box-outline",
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
        const findActiveDropdownIndex = navLinks.findIndex(
            (link) => link.items?.some((item) => location.pathname.includes(item.href))
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
                className={`fixed left-0 top-0 z-20 h-screen w-56 pt-20 transition-transform ${
                    isOpenSideBar ? "-translate-x-0" : "-translate-x-full"
                } border-r border-gray-200 bg-white dark:border-custom_lightblue dark:bg-custom_lightblue md:translate-x-0`}
                aria-label="Sidebar">
                <div className="h-full overflow-y-auto bg-white px-3 pb-4 dark:bg-custom_lightblue">
                    <ul className="space-y-2 font-normal">
                        {navLinks.map((link, index) => (
                            <li key={index}>
                                {link.items ? (
                                    <>
                                        <button
                                            onClick={() => toggleDropdown(index)}
                                            className="group flex w-full items-center rounded p-2 text-base text-gray-900 transition duration-75 hover:bg-gray-100 dark:text-white dark:hover:bg-custom_space_cadet_bis"
                                            aria-controls="dropdown"
                                            data-collapse-toggle="dropdown">
                                            <Icon icon={link.icon} width="20" height="20" />
                                            <span
                                                className="ml-3 flex-1 whitespace-nowrap text-left"
                                                sidebar-toggle-item="true">
                                                {link.text}
                                            </span>
                                            <Icon icon="mdi:chevron-down" width="20" height="20" />
                                        </button>
                                        {activeDropdownIndex === index && (
                                            <ul id="dropdown" className="space-y-2 py-2">
                                                {link.items.map((item, i) => (
                                                    <li key={i}>
                                                        <NavLink
                                                            to={item.href}
                                                            {...(item.href === Pathnames.Video.Video
                                                                ? { end: true }
                                                                : {})}
                                                            onClick={(e) => e.stopPropagation()}
                                                            className={({ isActive, isPending }) =>
                                                                `group flex w-full items-center rounded p-2 pl-11 text-base text-gray-900 transition duration-75 hover:bg-gray-100 dark:text-white dark:hover:bg-custom_space_cadet_bis ${
                                                                    isPending
                                                                        ? "pending"
                                                                        : isActive
                                                                          ? "dark:bg-custom_space_cadet_bis"
                                                                          : ""
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
                                            `flex items-center rounded p-2 text-base text-gray-900 hover:bg-custom_space_cadet_bis dark:text-white dark:hover:bg-custom_space_cadet_bis ${
                                                isPending
                                                    ? "pending"
                                                    : isActive
                                                      ? "dark:bg-custom_space_cadet_bis"
                                                      : ""
                                            }`
                                        }>
                                        <Icon icon={link.icon} width="20" height="20" />
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
