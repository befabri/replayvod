import React from "react";
import { useAuth } from "../../context/Auth/Auth";
import { useTranslation } from "react-i18next";
import IconButton from "../../components/UI/Button/IconButton";
import { Icon } from "@iconify/react";

const LoginPage: React.FC = () => {
    const { t } = useTranslation();
    const auth = useAuth();
    const handleClick = (event: React.MouseEvent<HTMLButtonElement>) => {
        event.preventDefault();
        auth.signIn();
    };

    return (
        <div className="flex min-h-screen">
            <div className="flex flex-col items-center justify-center w-full md:w-1/2 bg-custom_space_cadet">
                <h1 className="text-2xl font-semibold mb-4 text-white">{t("Sign in to ReplayVod")}</h1>
                <div>
                    <IconButton
                        icon={<Icon icon="mdi:twitch" width="18" height="18" />}
                        onClick={handleClick}
                        style="primary">
                        {t("Twitch connect")}
                    </IconButton>
                </div>
            </div>
            <div className="hidden md:block md:w-1/2 bg-custom_lightblue"></div>
        </div>
    );
};

export default LoginPage;
