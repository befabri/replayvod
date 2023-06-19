import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

interface Channel {
  broadcaster_id: string;
  broadcaster_login: string;
  broadcaster_name: string;
  followed_at: string;
  profile_picture?: string;
}

interface Stream {
  _id: string;
  id: string;
  user_id: string;
  user_login: string;
  user_name: string;
  game_id: string;
  game_name: string;
  type: string;
  title: string;
  tags: string[];
  viewer_count: number;
  started_at: string;
  language: string;
  thumbnail_url: string;
  tag_ids: string[];
  is_mature: boolean;
}

const Follows: React.FC = () => {
  const { t } = useTranslation();
  const [channels, setChannels] = useState<Channel[]>([]);
  const [streams, setStreams] = useState<Stream[]>([]);
  const [isLoading, setIsLoading] = useState<boolean>(true);

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [followedChannelsResponse, followedStreamsResponse] = await Promise.all([
          fetch("http://localhost:3000/api/users/me/followedchannels", { credentials: "include" }),
          fetch("http://localhost:3000/api/users/me/followedstreams", { credentials: "include" }),
        ]);

        const followedChannelsData = await followedChannelsResponse.json();
        const followedStreamsData = await followedStreamsResponse.json();

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
              (stream) => stream.user_id === channel.broadcaster_id && stream.type === "live"
            );

            return (
              <a
                className={`bg-zinc-100 dark:bg-gray-800 p-3 hover:bg-gray-100 dark:hover:bg-gray-700 ${
                  isLive ? "relative" : ""
                }`}
                href={`/channel/${channel.broadcaster_id}`}
                key={channel.broadcaster_id}
              >
                <div className="flex">
                  <img className="w-10 h-10 rounded-full" src={channel.profile_picture} alt="Profile Picture" />
                  <h2 className="flex dark:text-stone-100 items-center px-3">{channel.broadcaster_name}</h2>
                  {isLive && <div className="m-auto ml-0 w-4 h-4 bg-red-500 rounded-full align-center"></div>}
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
