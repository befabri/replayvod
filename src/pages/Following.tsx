import React, { useEffect, useState } from "react";

interface Channel {
  broadcaster_id: string;
  broadcaster_login: string;
  broadcaster_name: string;
  followed_at: string;
  profile_picture?: string;
}

const Follows: React.FC = () => {
  const [channels, setChannel] = useState<Channel[]>([]);
  const [isLoading, setIsLoading] = useState<boolean>(true);

  useEffect(() => {
    fetch("http://localhost:3000/api/users/me/followedchannels", {
      credentials: "include",
    })
      .then((response) => response.json())
      .then((data) => {
        console.log(data);
        setChannel(data);
        setIsLoading(false);
      })
      .catch((error) => {
        console.error("Error:", error);
        setIsLoading(false);
      });
  }, []);

  if (isLoading) {
    return <div>Chargement...</div>;
  }

  return (
    <div className="p-4 sm:ml-64">
      <div className="p-4 mt-14">
        <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">Chaines Suivies</h1>
        <div className="grid grid-cols-1 md:grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-5">
          {channels.map((channel) => (
            <a className="bg-zinc-100 dark:bg-gray-800 p-3 hover:bg-gray-100 dark:hover:bg-gray-700" href={`/channel/${channel.broadcaster_id}`}>
              <div className="flex" key={channel.broadcaster_id}>
                <img className="w-10 h-10 rounded-full" src={channel.profile_picture} alt="Profile Picture" />
                <h2 className="flex dark:text-stone-100 items-center px-3">{channel.broadcaster_name}</h2>
              </div>
            </a>
          ))}
        </div>
      </div>
    </div>
  );
};

export default Follows;
