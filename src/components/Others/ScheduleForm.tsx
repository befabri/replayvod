import React, { useState } from "react";
import { Controller, SubmitHandler, useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { ScheduleSchema } from "../../models/Schedule";
import type { ScheduleForm } from "../../models/Schedule";
import InputText from "../UI/Form/InputText";
import { useTranslation } from "react-i18next";
import Select from "../UI/Form/Select";
import InputNumber from "../UI/Form/InputNumber";
import { Category, Channel, Quality, Schedule, ScheduleDTO } from "../../type";
import Checkbox from "../UI/Form/CheckBox";
import { ApiRoutes, getApiRoute } from "../../type/routes";
import Button from "../UI/Button/Button";
import InputTag from "../UI/Form/InputTag";
import MultipleSelect from "../UI/Form/MultipleSelect";
import { customFetch } from "../../utils/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";

const MIN_TIME_BEFORE_DELETE = 10;
const MIN_VIEWERS_COUNT = 0;

interface ModalProps {
    onClose: any;
    onDelete: any;
}

interface ScheduleFormProps {
    defaultValue?: ScheduleDTO;
    modal?: ModalProps;
    scheduleId?: number;
    onDataChange?: (data: ScheduleForm) => void;
}

const ScheduleForm: React.FC<ScheduleFormProps> = ({
    modal,
    defaultValue = {
        isChannelNameDisabled: false,
        channelName: "",
        isDeleteRediff: false,
        hasTags: false,
        hasMinView: false,
        hasCategory: false,
        quality: "720",
        timeBeforeDelete: MIN_TIME_BEFORE_DELETE,
        viewersCount: MIN_VIEWERS_COUNT,
        categories: [],
        tags: [],
    },
    scheduleId,
}) => {
    const { t } = useTranslation();
    const isModal = !!modal;
    const [possibleMatches, setPossibleMatches] = useState<string[]>([]);
    const queryClient = useQueryClient();

    const {
        data: categories,
        isLoading: isLoadingCategories,
        isError: isErrorCategories,
    } = useQuery<Category[], Error>({
        queryKey: ["categories", "all"],
        queryFn: (): Promise<Category[]> => customFetch(ApiRoutes.GET_CATEGORY_ALL),
        staleTime: 5 * 60 * 1000,
    });

    const {
        data: channels,
        isLoading: isLoadingChannels,
        isError: isErrorChannels,
    } = useQuery<Channel[], Error>({
        queryKey: ["channels"],
        queryFn: (): Promise<Channel[]> => customFetch(ApiRoutes.GET_USER_FOLLOWED_CHANNELS),
        staleTime: 5 * 60 * 1000,
    });

    const postData = async (data: ScheduleForm): Promise<void> => {
        try {
            let url = "";
            let method = "";

            if (isModal) {
                url = getApiRoute(ApiRoutes.PUT_SCHEDULE_EDIT, "id", scheduleId);
                method = "PUT";
            } else {
                url = getApiRoute(ApiRoutes.POST_SCHEDULE);
                method = "POST";
            }

            const response = await fetch(url, {
                method: method,
                credentials: "include",
                headers: {
                    "Content-Type": "application/json",
                },
                body: JSON.stringify(data),
            });

            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            if (isModal) {
                const currentSchedules = queryClient.getQueryData<Schedule[]>(["schedules"]);
                if (currentSchedules) {
                    const updatedSchedules = currentSchedules.map((schedule) =>
                        schedule.id === scheduleId ? { ...schedule, ...data } : schedule
                    );
                    queryClient.setQueryData(["schedules"], updatedSchedules);
                }
            }
        } catch (error) {
            console.error(`Error posting data: ${error}`);
        }
    };

    const checkChannelNameValidity = async (channelName: string) => {
        try {
            const url = getApiRoute(ApiRoutes.GET_CHANNEL_NAME_NAME, "name", channelName);
            const response = await fetch(url, {
                credentials: "include",
            });

            if (!response.ok) {
                const data = await response.json();
                throw new Error(data.error || `HTTP error! status: ${response.status}`);
            }

            const data = await response.json();
            return data.exists;
        } catch (error) {
            console.error(`Error fetching data: ${error}`);
            return false;
        }
    };

    const {
        register,
        handleSubmit,
        setError,
        clearErrors,
        trigger,
        control,
        formState: { errors },
        watch,
    } = useForm<ScheduleForm>({
        resolver: zodResolver(ScheduleSchema),
        defaultValues: {
            channelName: defaultValue.channelName,
            isDeleteRediff: defaultValue.isDeleteRediff,
            hasTags: defaultValue.hasTags,
            hasMinView: defaultValue.hasMinView,
            hasCategory: defaultValue.hasCategory,
            quality: defaultValue.quality,
            timeBeforeDelete: defaultValue.timeBeforeDelete
                ? defaultValue.timeBeforeDelete
                : MIN_TIME_BEFORE_DELETE,
            viewersCount: defaultValue.viewersCount ? defaultValue.viewersCount : MIN_VIEWERS_COUNT,
            categories: defaultValue.categories,
            tags: defaultValue.tags,
        },
    });
    const channelName = watch("channelName");
    const isDeleteRediff = watch("isDeleteRediff");
    const hasTags = watch("hasTags");
    const hasMinView = watch("hasMinView");
    const hasCategory = watch("hasCategory");

    const onSubmit: SubmitHandler<ScheduleForm> = async (data) => {
        const exists = await checkChannelNameValidity(data.channelName);
        if (!exists) {
            setError("channelName", {
                type: "manual",
                message: "Channel name doesn't exist",
            });
            return;
        }
        console.log(data);
        postData(data);
    };

    if (isLoadingCategories || isLoadingChannels) {
        return <div>{t("Loading")}</div>;
    }

    if (isErrorCategories || isErrorChannels) {
        return <div>{t("Error")}</div>;
    }

    if (!channels || !categories) {
        return <div>{t("Error")}</div>;
    }

    const handleBlur = async (fieldName: keyof ScheduleForm) => {
        const isValid = await trigger(fieldName);
        if (!isValid) return;
        if (fieldName === "channelName") {
            const exists = await checkChannelNameValidity(channelName);
            if (!exists) {
                setError("channelName", {
                    type: "manual",
                    message: "Channel name dont exist",
                });
            } else {
                clearErrors("channelName");
            }
        }
    };

    const handleChange = async (fieldName: keyof ScheduleForm, value: string) => {
        if (fieldName === "channelName") {
            if (value.length > 0) {
                const matches = channels
                    .filter((channel) => channel?.broadcasterName?.toLowerCase().startsWith(value.toLowerCase()))
                    .map((channel) => channel.broadcasterName);
                setPossibleMatches(matches);
            } else {
                setPossibleMatches([]);
            }
        }
    };

    return (
        <form onSubmit={handleSubmit(onSubmit)}>
            <div className=" p-4 md:p-5">
                <InputText
                    label={t("Channel Name")}
                    id="channelName"
                    placeholder={t("Channel Name")}
                    required={true}
                    list="possible-matches"
                    register={register("channelName")}
                    disabled={defaultValue.isChannelNameDisabled}
                    error={errors.channelName}
                    onBlur={() => handleBlur("channelName")}
                    onChange={(e: { target: { value: string } }) => handleChange("channelName", e.target.value)}
                />
                <datalist id="possible-matches">
                    {possibleMatches.map((match, index) => (
                        <option key={index} value={match} />
                    ))}
                </datalist>

                <div className="mt-5">
                    <Select
                        label={t("Video quality")}
                        id="quality"
                        register={register("quality", { required: true })}
                        required={true}
                        error={errors.quality}
                        options={[Quality.LOW, Quality.MEDIUM, Quality.HIGH]}
                    />
                </div>
                <div className="mt-5">
                    <Checkbox
                        label={t("Deletion of the video if the VOD is kept after the stream")}
                        helperText={t("Set the stream end time in minutes before the VOD is suppressed")}
                        id="isDeleteRediff"
                        error={errors.isDeleteRediff}
                        register={register("isDeleteRediff")}
                    />
                    <InputNumber
                        id="timeBeforeDelete"
                        register={register("timeBeforeDelete")}
                        error={errors.timeBeforeDelete}
                        required={false}
                        disabled={!isDeleteRediff}
                        minValue={MIN_TIME_BEFORE_DELETE}
                    />
                </div>

                <div className="mt-5">
                    <Checkbox
                        label={t("Minimum number of views")}
                        id="hasMinView"
                        error={errors.hasMinView}
                        register={register("hasMinView")}
                    />
                    <InputNumber
                        id="viewersCount"
                        register={register("viewersCount")}
                        error={errors.viewersCount}
                        required={false}
                        disabled={!hasMinView}
                        minValue={MIN_VIEWERS_COUNT}
                    />
                </div>
                <div className="mt-5">
                    <Checkbox
                        label={t("Game category")}
                        id="hasCategory"
                        error={errors.hasCategory}
                        register={register("hasCategory")}
                    />
                    <Controller
                        name="categories"
                        control={control}
                        render={({ field }) => (
                            <MultipleSelect
                                id="categories"
                                error={errors.categories}
                                options={categories.map((category) => category.name)}
                                disabled={!hasCategory}
                                onCategoriesChange={field.onChange}
                                value={field.value}
                            />
                        )}
                    />
                </div>
                <div className="mt-5">
                    <Checkbox
                        label={t("Twitch tags")}
                        id="hasTags"
                        error={errors.hasTags}
                        register={register("hasTags")}
                    />

                    <Controller
                        name="tags"
                        control={control}
                        render={({ field }) => (
                            <InputTag
                                id="tags"
                                error={errors.tags}
                                disabled={!hasTags}
                                onTagsChange={field.onChange}
                                value={field.value}
                            />
                        )}
                    />
                </div>
            </div>
            {!isModal && (
                <div className="mt-5">
                    <Button
                        text={t("Add Schedule")}
                        typeButton="submit"
                        disabled={Object.keys(errors).length > 0}
                    />
                </div>
            )}
            {isModal && (
                <div className="mt-4 flex items-center justify-between rounded-b border-t-2  border-gray-200 p-4 dark:border-custom_delft_blue md:p-5">
                    <Button onClick={modal.onDelete} style="primary">
                        {t("Delete")}
                    </Button>
                    <div className="flex gap-3">
                        <Button onClick={modal.onClose} style="primary">
                            {t("Cancel")}
                        </Button>
                        <Button
                            text={t("Save")}
                            typeButton="submit"
                            style="submit"
                            disabled={Object.keys(errors).length > 0}
                        />
                    </div>
                </div>
            )}
        </form>
    );
};

export default ScheduleForm;
