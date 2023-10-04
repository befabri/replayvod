import React, { useState, useEffect } from "react";
import { SubmitHandler, useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { ScheduleSchema } from "../../models/Schedule";
import type { ScheduleForm } from "../../models/Schedule";
import InputText from "../../components/Form/InputText";
import { useTranslation } from "react-i18next";
import Select from "../../components/Form/Select";
import InputNumber from "../../components/Form/InputNumber";
import { Category, Channel, Quality } from "../../type";
import Checkbox from "../../components/Form/CheckBox";
import { ApiRoutes, getApiRoute } from "../../type/routes";
const AddChannel: React.FC = () => {
    const { t } = useTranslation();
    const [categories, setCategories] = useState<Category[]>([]);
    const [isLoading, setIsLoading] = useState<boolean>(true);
    const [channels, setChannels] = useState<Channel[]>([]);
    const [possibleMatches, setPossibleMatches] = useState<string[]>([]);
    const minTimeBeforeDelete = 10;
    const minViewersCount = 0;

    useEffect(() => {
        const fetchData = async () => {
            const urlCategory = getApiRoute(ApiRoutes.GET_CATEGORY);
            const urlFollowedChannels = getApiRoute(ApiRoutes.GET_USER_FOLLOWED_CHANNELS);

            try {
                const [categoryResponse, followedChannelsResponse] = await Promise.all([
                    fetch(urlCategory, { credentials: "include" }),
                    fetch(urlFollowedChannels, { credentials: "include" }),
                ]);

                if (!categoryResponse.ok || !followedChannelsResponse.ok) {
                    throw new Error("HTTP error");
                }

                const [categoryData, followedChannelsData] = await Promise.all([
                    categoryResponse.json(),
                    followedChannelsResponse.json(),
                ]);

                setCategories(categoryData);
                setValue("category", categoryData.length ? categoryData[0].name : "");
                setChannels(followedChannelsData);
            } catch (error) {
                console.error(`Error fetching data: ${error}`);
            } finally {
                setIsLoading(false);
            }
        };

        fetchData();
    }, []);

    const postData = async (data: ScheduleForm) => {
        try {
            let url = getApiRoute(ApiRoutes.POST_DOWNLOAD_SCHEDULE);
            const response = await fetch(url, {
                method: "POST",
                credentials: "include",
                headers: {
                    "Content-Type": "application/json",
                },
                body: JSON.stringify(data),
            });

            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            console.log(response);
        } catch (error) {
            console.error(`Error posting data: ${error}`);
        }
    };

    const checkChannelNameValidity = async (channelName: string) => {
        try {
            let url = getApiRoute(ApiRoutes.GET_CHANNEL_NAME_NAME, "name", channelName);
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
        // formState: { errors, isValid },
        formState: { errors },
        watch,
        setValue,
    } = useForm<ScheduleForm>({
        resolver: zodResolver(ScheduleSchema),
        defaultValues: {
            isDeleteRediff: false,
            hasTags: false,
            hasMinView: false,
            hasCategory: false,
            quality: Quality.LOW,
            category: categories.length ? categories[0].name : "",
            timeBeforeDelete: minTimeBeforeDelete,
            viewersCount: minViewersCount,
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
        postData(data);
    };

    // const allValues = watch();
    // console.log(allValues);
    // console.log("Est valide: %s", isValid);

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

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    return (
        <div className="p-4">
            <div className="p-4 mt-14">
                <form onSubmit={handleSubmit(onSubmit)}>
                    <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Schedule")}</h1>
                    <div className="mt-5">
                        <InputText
                            label={t("Channel Name")}
                            id="channelName"
                            placeholder={t("Channel Name")}
                            required={true}
                            list="possible-matches"
                            register={register("channelName")}
                            error={errors.channelName}
                            onBlur={() => handleBlur("channelName")}
                            onChange={(e: { target: { value: string } }) =>
                                handleChange("channelName", e.target.value)
                            }
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
                                minValue={minTimeBeforeDelete}
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
                                minValue={minViewersCount}
                            />
                        </div>
                        <div className="mt-5">
                            <Checkbox
                                label={t("Game category")}
                                id="hasCategory"
                                error={errors.hasCategory}
                                register={register("hasCategory")}
                            />
                            <Select
                                id="category"
                                register={register("category", { required: true })}
                                required={false}
                                error={errors.category}
                                options={categories.map((category) => category.name)}
                                disabled={!hasCategory}
                            />
                        </div>
                        <div className="mt-5">
                            <Checkbox
                                label={t("Twitch tags")}
                                id="hasTags"
                                error={errors.hasTags}
                                register={register("hasTags")}
                            />
                            <InputText
                                id="tag"
                                placeholder={t("Twitch tags separate by ,")}
                                required={false}
                                list=""
                                register={register("tag")}
                                error={errors.tag}
                                onBlur={() => handleBlur("tag")}
                                disabled={!hasTags}
                            />
                        </div>
                        <button
                            type="submit"
                            disabled={Object.keys(errors).length > 0}
                            className="mt-10 text-3xl bg-gray-300 p-2 rounded-md max-w-[10rem]">
                            Submit
                        </button>
                    </div>
                </form>
            </div>
        </div>
    );
};

export default AddChannel;
