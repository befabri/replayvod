import React, { useState, useEffect, useRef } from "react";
import { Icon } from "@iconify/react";
import { useTranslation } from "react-i18next";

const Sidebar: React.FC = () => {
  const { t } = useTranslation();
  const [isOpen, setIsOpen] = useState(false);
  const toggle = () => {
    setIsOpen(!isOpen);
    console.log("Button toggled. Current state:", !isOpen ? "Open" : "Closed");
  };
  const dropdownRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const pageClickEvent = (e: MouseEvent) => {
      if (dropdownRef.current !== null && !dropdownRef.current.contains(e.target as Node)) {
        setIsOpen(!isOpen);
      }
    };

    if (isOpen) {
      window.addEventListener("click", pageClickEvent);
    }

    return () => {
      window.removeEventListener("click", pageClickEvent);
    };
  }, [isOpen]);

  return (
    <>
      <aside
        id="logo-sidebar"
        className="fixed top-0 left-0 z-40 w-64 h-screen pt-20 transition-transform -translate-x-full bg-white border-r border-gray-200 sm:translate-x-0 dark:bg-gray-800 dark:border-gray-700"
        aria-label="Sidebar"
      >
        <div className="h-full px-3 pb-4 overflow-y-auto bg-white dark:bg-gray-800">
          <ul className="space-y-2 font-normal">
            <li>
              <a
                href="/"
                className="flex items-center p-2 text-gray-900 rounded-lg dark:text-white hover:bg-gray-100 dark:hover:bg-gray-700"
              >
                <Icon icon="mdi:view-dashboard" width="18" height="18" />
                <span className="ml-3">{t("Dashboard")}</span>
              </a>
            </li>
            <li>
              <a
                href="/vod"
                className="flex items-center p-2 text-gray-900 rounded-lg dark:text-white hover:bg-gray-100 dark:hover:bg-gray-700"
              >
                <Icon icon="mdi:play" width="18" height="18" />
                <span className="ml-3">{t("Videos")}</span>
              </a>
            </li>
            <li>
              <button
                onClick={toggle}
                className="flex items-center w-full p-2 text-gray-900 transition duration-75 rounded-lg group hover:bg-gray-100 dark:text-white dark:hover:bg-gray-700"
                aria-controls="dropdown-example"
                data-collapse-toggle="dropdown-example"
              >
                <Icon icon="mdi:tray-arrow-down" width="18" height="18" />
                <span className="flex-1 ml-3 text-left whitespace-nowrap" sidebar-toggle-item>
                  {t("Recording")}
                </span>
                <Icon icon="mdi:chevron-down" width="18" height="18" />
              </button>
              {isOpen && (
                <ul id="dropdown-example" className="py-2 space-y-2">
                  <li>
                    <a
                      href="/add"
                      className="flex items-center w-full p-2 text-gray-900 transition duration-75 rounded-lg pl-11 group hover:bg-gray-100 dark:text-white dark:hover:bg-gray-700"
                    >
                      {t("Schedule Videos")}
                    </a>
                  </li>
                  <li>
                    <a
                      href="/following"
                      className="flex items-center w-full p-2 text-gray-900 transition duration-75 rounded-lg pl-11 group hover:bg-gray-100 dark:text-white dark:hover:bg-gray-700"
                    >
                      {t("Followed Channels")}
                    </a>
                  </li>
                </ul>
              )}
            </li>
            <li>
              <a
                href="/settings"
                className="flex items-center p-2 text-gray-900 rounded-lg dark:text-white hover:bg-gray-100 dark:hover:bg-gray-700"
              >
                <Icon icon="mdi:cog" width="18" height="18" />
                <span className="flex-1 ml-3 whitespace-nowrap">{t("Settings")}</span>
              </a>
            </li>
          </ul>
        </div>
      </aside>
    </>
  );
};

export default Sidebar;
