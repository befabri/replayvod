import React, { useEffect, useState } from "react";

interface Stream {
  _id: string;
  user_name: string;
  title: string;
  profilePicture: string;
}

const Follows: React.FC = () => {
  const [streams, setStreams] = useState<Stream[]>([]);
  const [isLoading, setIsLoading] = useState<boolean>(true);

  useEffect(() => {
    fetch("http://localhost:3000/api/user/follows", {
      credentials: "include", // Needed to include the session cookie
    })
      .then((response) => response.json())
      .then((data) => {
        // If the request was successful, update streams
        console.log(data);
        setStreams(data);
        setIsLoading(false);
      })
      .catch((error) => {
        console.error("Error:", error);
        setIsLoading(false);
      });
  }, []);

  if (isLoading) {
    return <div>Loading...</div>;
  }

  return (
    <div className="p-4 sm:ml-64">
      <div className="p-4 mt-14">
        <h1 className="text-3xl font-bold pb-5">Chaines Suivies</h1>
        <div className="grid grid-cols-1 md:grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-4">
          {streams.map((stream) => (
            <div className="flex custom-font-color p-3" key={stream._id}>
              <img className="w-10 h-10 rounded-full" src={stream.profilePicture} alt="Profile Picture" />
              <h2 className="flex items-center px-3">{stream.user_name}</h2>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};

export default Follows;
