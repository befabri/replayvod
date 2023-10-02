import React, { SetStateAction, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Channel, Stream } from "../../type";
import { ApiRoutes, Pathnames, getApiRoute } from "../../type/routes";
import DropdownButton from "../../components/UI/Button/ButtonDropdown";

const Follows: React.FC = () => {
    const { t } = useTranslation();
    const [channels, setChannels] = useState<Channel[]>([]);
    const [streams, setStreams] = useState<Stream[]>([]);
    const [order, setOrder] = useState("Channel (ascending)");
    const [isLoading, setIsLoading] = useState<boolean>(true);

    useEffect(() => {
        const fetchData = async () => {
            try {
                let urlFollowedChannels = getApiRoute(ApiRoutes.GET_USER_FOLLOWED_CHANNELS);
                let urlFollowedStreams = getApiRoute(ApiRoutes.GET_USER_FOLLOWED_STREAMS);
                const [followedChannelsResponse, followedStreamsResponse] = await Promise.all([
                    fetch(urlFollowedChannels, { credentials: "include" }),
                    fetch(urlFollowedStreams, { credentials: "include" }),
                ]);

                const followedChannelsData: SetStateAction<Channel[]> = await followedChannelsResponse.json();
                const followedStreamsData: SetStateAction<Stream[]> = await followedStreamsResponse.json();

                setChannels(followedChannelsData);
                setStreams(followedStreamsData);
                setIsLoading(false);
            } catch (error) {
                console.error("Error:", error);
                setIsLoading(false);
            }
        };

        fetchData();
    }, []);

    useEffect(() => {
        if (order === t("Channel (ascending)")) {
            const sortedChannels = [...channels].sort((a, b) =>
                a.broadcasterName.localeCompare(b.broadcasterName)
            );
            setChannels(sortedChannels);
        } else if (order === t("Channel (descending)")) {
            const sortedChannels = [...channels].sort((b, a) =>
                a.broadcasterName.localeCompare(b.broadcasterName)
            );
            setChannels(sortedChannels);
        } else {
            const sortedChannels = [...channels].sort((a, b) =>
                a.broadcasterName.localeCompare(b.broadcasterName)
            );
            setChannels(sortedChannels);
        }
    }, [order]);

    const handleOrderSelected = (value: any) => {
        setOrder(value);
    };

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    return (
        <div className="p-4">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Followed Channels")}</h1>
                <div className="flex mb-4 items-center justify-end space-x-5">
                    <div className="space-x-2">
                        <DropdownButton
                            label={t(order)}
                            options={[t("Channel (ascending)"), t("Channel (descending)")]}
                            onOptionSelected={handleOrderSelected}
                        />
                    </div>
                </div>
                <div className="grid grid-cols-1 md:grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-5">
                    {channels.map((channel) => {
                        const isLive = streams.some(
                            (stream) => stream.broadcasterId === channel.broadcasterId && stream.type === "live"
                        );

                        return (
                            <a
                                className={`bg-zinc-100 dark:bg-gray-800 p-3 hover:bg-gray-100 dark:hover:bg-gray-700 ${
                                    isLive ? "relative" : ""
                                }`}
                                href={`${Pathnames.Channel}${channel.broadcasterId}`}
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
                            </a>
                        );
                    })}
                </div>
            </div>
        </div>
    );
};
export default Follows;
