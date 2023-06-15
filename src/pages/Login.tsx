import React from "react";
import { useAuth } from "./Auth";

const Landing: React.FC = () => {
  let auth = useAuth();
  const handleClick = (event: React.MouseEvent<HTMLButtonElement>) => {
    event.preventDefault();
    auth.signin();
  }

  return (
    <div className="flex min-h-screen">
      <div className="flex flex-col items-center justify-center w-full md:w-1/2 bg-gray-100 mb-10">
        <h1 className="text-2xl font-medium mb-4">Sign in to Replay</h1>
        <button
          onClick={handleClick}
          className="px-6 py-2 text-white bg-violet-500 rounded-md hover:bg-violet-700"
        >
          Twitch connect
        </button>
      </div>
      <div className="hidden md:block md:w-1/2 bg-violet-900"></div>
    </div>
  );
};

export default Landing;
