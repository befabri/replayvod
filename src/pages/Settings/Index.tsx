import React, { useState, useEffect } from "react";
import { SubmitHandler, useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { SettingsSchema } from "../../models/Settings";
import type { SettingsForm } from "../../models/Settings";
import { useTranslation } from "react-i18next";
import Select from "../../components/Form/Select";
import { ApiRoutes, getApiRoute } from "../../type/routes";
import { DateTimeFormats, Settings } from "../../type";
import Button from "../../components/Form/Button";
import { timeZones } from "../../utils/timezones";

const SettingsPage: React.FC = () => {
    const { t } = useTranslation();
    const [isLoading, setIsLoading] = useState<boolean>(true);

    const fetchData = async () => {
        try {
            const url = getApiRoute(ApiRoutes.GET_SETTINGS);
            const response = await fetch(url, { credentials: "include" });

            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }

            const data: Settings = await response.json();
            setValue("timeZone", data.timeZone ? data.timeZone : "Europe/London");
            setValue("dateTimeFormat", data.dateTimeFormat ? data.dateTimeFormat : DateTimeFormats[0]);
            setIsLoading(false);
        } catch (error) {
            console.error(`Error fetching data: ${error}`);
        }
    };

    useEffect(() => {
        const storedTimeZone = localStorage.getItem("timeZone");
        const storedDateTimeFormat = localStorage.getItem("dateTimeFormat");

        if (storedTimeZone && storedDateTimeFormat) {
            setValue("timeZone", storedTimeZone);
            setValue("dateTimeFormat", storedDateTimeFormat);
            setIsLoading(false);
        } else {
            fetchData();
        }
    }, []);

    const postData = async (data: SettingsForm) => {
        try {
            const url = getApiRoute(ApiRoutes.POST_SETTINGS);
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
            if (response.ok) {
                localStorage.setItem("timeZone", data.timeZone);
                localStorage.setItem("dateTimeFormat", data.dateTimeFormat);
            }
        } catch (error) {
            console.error(`Error posting data: ${error}`);
        }
    };

    const {
        register,
        handleSubmit,
        formState: { errors },
        setValue,
    } = useForm<SettingsForm>({
        resolver: zodResolver(SettingsSchema),
        defaultValues: {
            timeZone: "Europe/London",
            dateTimeFormat: DateTimeFormats[0],
        },
    });

    const onSubmit: SubmitHandler<SettingsForm> = async (data) => {
        postData(data);
    };

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    return (
        <div className="p-4">
            <div className="p-4 mt-14">
                <form onSubmit={handleSubmit(onSubmit)}>
                    <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Settings")}</h1>
                    <div className="mt-5">
                        <div className="mt-5">
                            <Select
                                label={t("Time Zone")}
                                id="timeZone"
                                register={register("timeZone", { required: true })}
                                required={false}
                                error={errors.timeZone}
                                options={timeZones}
                            />
                        </div>
                        <div className="mt-5">
                            <Select
                                label={t("DateTime Format")}
                                id="dateTimeFormat"
                                register={register("dateTimeFormat", { required: true })}
                                required={false}
                                error={errors.dateTimeFormat}
                                options={DateTimeFormats}
                            />
                        </div>
                        <div className="mt-5">
                            <Button text={t("Export settings")} typeButton="submit" />
                        </div>
                    </div>
                </form>
            </div>
        </div>
    );
};

export default SettingsPage;
