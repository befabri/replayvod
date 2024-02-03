import React, { useEffect } from "react";
import { SubmitHandler, useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { SettingsSchema } from "../../models/Settings";
import type { SettingsForm } from "../../models/Settings";
import { useTranslation } from "react-i18next";
import Select from "../../components/UI/Form/Select";
import { ApiRoutes, getApiRoute } from "../../type/routes";
import { DateTimeFormats } from "../../type";
import { timeZones } from "../../utils/timezones";
import Button from "../../components/UI/Button/Button";
import { useQuery } from "@tanstack/react-query";
import Container from "../../components/Layout/Container";
import Title from "../../components/Typography/TitleComponent";

const fetchSettings = async (): Promise<SettingsForm> => {
    const url = getApiRoute(ApiRoutes.GET_SETTINGS);
    const response = await fetch(url, { credentials: "include" });
    if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
    }
    return response.json();
};

const postSettings = async (data: SettingsForm) => {
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
        localStorage.setItem("timeZone", data.timeZone);
        localStorage.setItem("dateTimeFormat", data.dateTimeFormat);
        return response.json();
    } catch (error) {
        console.error(`Error posting data: ${error}`);
    }
};

const SettingsPage: React.FC = () => {
    const { t } = useTranslation();

    const { data, isLoading, isError } = useQuery<SettingsForm, Error>({
        queryKey: ["settings"],
        queryFn: () => fetchSettings(),
        staleTime: 5 * 60 * 1000,
        enabled: !localStorage.getItem("timeZone") || !localStorage.getItem("dateTimeFormat"),
    });

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

    useEffect(() => {
        const storedTimeZone = localStorage.getItem("timeZone");
        const storedDateTimeFormat = localStorage.getItem("dateTimeFormat");

        if (storedTimeZone && storedDateTimeFormat) {
            setValue("timeZone", storedTimeZone);
            setValue("dateTimeFormat", storedDateTimeFormat);
        } else if (data) {
            setValue("timeZone", data.timeZone || "Europe/London");
            setValue("dateTimeFormat", data.dateTimeFormat || DateTimeFormats[0]);
        }
    }, [data, setValue]);

    const onSubmit: SubmitHandler<SettingsForm> = async (data) => {
        postSettings(data);
    };

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (isError) return <div>{t("Error")}</div>;

    return (
        <Container>
            <Title title={t("Settings")} />
            <form onSubmit={handleSubmit(onSubmit)}>
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
        </Container>
    );
};

export default SettingsPage;
