import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiRoutes, Pathnames, getApiRoute } from "../../../type/routes";
import { Link, useNavigate } from "react-router-dom";
import { Channel, Stream } from "../../../type";
import DropdownButton from "../../../components/UI/Button/ButtonDropdown";

const ChannelPage: React.FC = () => {
    const { t } = useTranslation();
    const [channels, setChannels] = useState<Channel[]>([]);
    const [streams, setStreams] = useState<Stream[]>([]);
    const [order, setOrder] = useState({ value: "channel_asc", label: t("Channel (A-Z)") });
    const [isLoading, setIsLoading] = useState<boolean>(true);
    const navigate = useNavigate();

    const orderOptions = [
        { value: "channel_asc", label: t("Channel (A-Z)") },
        { value: "channel_desc", label: t("Channel (Z-A)") },
    ];

    const fetchData = async () => {
        const urlFollowedChannels = getApiRoute(ApiRoutes.GET_USER_FOLLOWED_CHANNELS);
        const urlFollowedStreams = getApiRoute(ApiRoutes.GET_USER_FOLLOWED_STREAMS);
        try {
            const [followedChannelsResponse, followedStreamsResponse] = await Promise.all([
                fetch(urlFollowedChannels, { credentials: "include" }),
                fetch(urlFollowedStreams, { credentials: "include" }),
            ]);

            if (!followedChannelsResponse.ok || !followedStreamsResponse.ok) {
                throw new Error("HTTP error");
            }

            const [followedChannelsData, followedStreamsData] = await Promise.all([
                followedChannelsResponse.json(),
                followedStreamsResponse.json(),
            ]);

            const params = new URLSearchParams(location.search);
            const sortParam = params.get("sort");
            const newOrder = orderOptions.find((option) => option.value === sortParam) || {
                value: "channel_asc",
                label: t("Channel (A-Z)"),
            };
            setOrder(newOrder);
            sortChannels(followedChannelsData, newOrder.value);
            setIsLoading(false);
            setStreams(followedStreamsData);
        } catch (error) {
            console.error("Error:", error);
            setIsLoading(false);
        }
    };

    const sortChannels = (channels: Channel[], order: string) => {
        const sortedVideos = [...channels];
        if (order === "channel_desc") {
            sortedVideos.sort((b, a) => a.broadcasterName.localeCompare(b.broadcasterName));
        } else {
            sortedVideos.sort((a, b) => a.broadcasterName.localeCompare(b.broadcasterName));
        }
        setChannels(sortedVideos);
    };

    useEffect(() => {
        fetchData();
        const intervalId = setInterval(fetchData, 120000);
        return () => clearInterval(intervalId);
    }, []);

    const handleOrderSelected = (value: string) => {
        const selectedOption = orderOptions.find((option) => option.value === value);
        if (selectedOption) {
            setOrder(selectedOption);
            sortChannels(channels, selectedOption.value);
            navigate(`${location.pathname}?sort=${value}`);
        }
    };

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    return (
        <div className="p-4">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Channels")}</h1>
                <div className="flex mb-4 items-center justify-end space-x-5">
                    <div className="space-x-2">
                        <DropdownButton
                            label={t(order.label)}
                            options={orderOptions}
                            onOptionSelected={handleOrderSelected}
                        />
                    </div>
                </div>
                <div className="grid grid-cols-1 md:grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-3">
                    {channels.map((channel) => {
                        const isLive = streams.some(
                            (stream) => stream.broadcasterId === channel.broadcasterId && stream.type === "live"
                        );

                        return (
                            <Link
                                to={`${Pathnames.Video.Channel}/${channel.broadcasterLogin}`}
                                className={`bg-zinc-100 dark:bg-custom_lightblue p-3 hover:bg-gray-100 dark:hover:bg-custom_vista_blue ${
                                    isLive ? "relative" : ""
                                }`}
                                key={channel.broadcasterId}>
                                <div className="flex">
                                    <img
                                        className="w-10 h-10 rounded-full"
                                        src={channel.profilePicture}
                                        alt="Profile Picture"
                                    />
                                    <h2 className="flex dark:text-stone-100 items-center px-3">
                                        {channel.broadcasterName}
                                    </h2>
                                    {isLive && (
                                        <div className="m-auto ml-0 w-4 h-4 bg-red-500 rounded-full align-center"></div>
                                    )}
                                </div>
                            </Link>
                        );
                    })}
                </div>
            </div>
        </div>
    );
};
export default ChannelPage;
