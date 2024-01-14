import React, { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ManageSchedule } from "../../../type";
import { Icon } from "@iconify/react/dist/iconify.js";
import ScheduleForm from "../../Others/ScheduleForm";
import { ApiRoutes, getApiRoute } from "../../../type/routes";
import { qualityLabelToResolution } from "../../../utils/utils";

interface ScheduleModalProps {
    isOpen: boolean;
    onClose: () => void;
    data: ManageSchedule;
    onScheduleDelete: (scheduleId: number) => void;
}

const ScheduleModal: React.FC<ScheduleModalProps> = ({ isOpen, onClose, onScheduleDelete, data }) => {
    const { t } = useTranslation();
    const modalRef = useRef<HTMLDivElement>(null);

    const [modalData, setModalData] = useState<ManageSchedule | null>(null);
    useEffect(() => {
        if (data) {
            setModalData(data);
        }
    }, [data]);

    useEffect(() => {
        const handleClickOutside = (event: MouseEvent) => {
            const target = event.target as Node;
            if (modalRef.current && !modalRef.current.contains(target)) {
                onClose();
            }
        };
        document.addEventListener("mousedown", handleClickOutside);
        return () => {
            document.removeEventListener("mousedown", handleClickOutside);
        };
    }, [modalRef, onClose]);

    const removeSchedule = async (scheduleId: number) => {
        try {
            const url = getApiRoute(ApiRoutes.DELETE_DOWNLOAD_SCHEDULE, "id", scheduleId);
            const response = await fetch(url, {
                method: "DELETE",
                credentials: "include",
                headers: {
                    "Content-Type": "application/json",
                },
            });

            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            return true;
        } catch (error) {
            console.error(`Error posting data: ${error}`);
            return false;
        }
    };

    const onDelete = async () => {
        if (!modalData) {
            return null;
        }
        const result = await removeSchedule(modalData.id);
        if (result) {
            onScheduleDelete(modalData.id);
            onClose();
        }
    };

    if (!isOpen || !modalData) return null;

    return (
        <div className="fixed inset-0 z-50 flex w-full items-center justify-center overflow-y-auto overflow-x-hidden">
            <div ref={modalRef} className="relative max-h-full w-full max-w-xl p-4">
                <div className="relative rounded-sm bg-white shadow dark:bg-custom_space_cadet">
                    <div className="flex items-center justify-between rounded-t border-b-2 p-4 dark:border-custom_delft_blue md:p-5">
                        <h3 className="text-xl font-semibold text-gray-900 dark:text-white">
                            {t("Edit Schedule")} - {modalData.channel.displayName}
                        </h3>
                        <button
                            onClick={onClose}
                            className="ms-auto inline-flex h-8 w-8 items-center justify-center rounded-lg p-1 text-gray-900 transition duration-75 hover:bg-gray-100 dark:text-white dark:hover:bg-custom_vista_blue"
                            aria-controls="Close modal">
                            <Icon icon="mdi:close" width="18" height="18" />
                        </button>
                    </div>
                    <div className="space-y-4">
                        <ScheduleForm
                            modal={{
                                onClose,
                                onDelete,
                            }}
                            defaultValue={{
                                isChannelNameDisabled: true,
                                channelName: modalData.channel.broadcasterLogin || "",
                                timeBeforeDelete: modalData.timeBeforeDelete || 10,
                                viewersCount: modalData.viewersCount || 0,
                                category:
                                    modalData.downloadScheduleCategory?.map(
                                        (categoryObj) => categoryObj.category
                                    ) || [],
                                quality: qualityLabelToResolution(modalData.quality),
                                isDeleteRediff: modalData.isDeleteRediff,
                                hasTags: modalData.downloadScheduleTag && modalData.downloadScheduleTag.length > 0,
                                tags: modalData.downloadScheduleTag?.map((tagObj) => tagObj.tag) || [],
                                hasMinView: (modalData.viewersCount ?? 0) > 0 || false,
                                hasCategory: !!modalData.downloadScheduleCategory?.[0]?.category,
                            }}
                            scheduleId={modalData.id}
                        />
                    </div>
                </div>
            </div>
        </div>
    );
};

export default ScheduleModal;
