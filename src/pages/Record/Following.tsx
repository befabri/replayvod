import React, { SetStateAction, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Channel, Stream } from "../../type";

const Follows: React.FC = () => {
    const { t } = useTranslation();
    const [channels, setChannels] = useState<Channel[]>([]);
    const [streams, setStreams] = useState<Stream[]>([]);
    const [isLoading, setIsLoading] = useState<boolean>(true);
    const ROOT_URL = import.meta.env.VITE_ROOTURL;

    useEffect(() => {
        const fetchData = async () => {
            try {
                const [followedChannelsResponse, followedStreamsResponse] = await Promise.all([
                    fetch(`${ROOT_URL}/api/users/me/followedchannels`, { credentials: "include" }),
                    fetch(`${ROOT_URL}/api/users/me/followedstreams`, { credentials: "include" }),
                ]);

                const followedChannelsData: SetStateAction<Channel[]> = await followedChannelsResponse.json();
                const followedStreamsData: SetStateAction<Stream[]> = await followedStreamsResponse.json();

                console.log("Followed Channels:", followedChannelsData);
                console.log("Followed Streams:", followedStreamsData);

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

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    return (
        <div className="p-4 sm:ml-64">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Followed Channels")}</h1>
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
                                href={`/channel/${channel.broadcasterId}`}
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
