import React, { useEffect, useState } from "react";
import { useParams } from "react-router-dom";

interface User {
  id: string;
  login: string;
  display_name: string;
  type: string;
  broadcaster_type: string;
  description: string;
  profile_image_url: string;
  offline_image_url: string;
  view_count: number;
  email: string;
  created_at: string;
}

const Channel: React.FC = () => {
  let { id } = useParams();

  const [user, setUser] = useState<User>();
  const [isLoading, setIsLoading] = useState<boolean>(true);

  useEffect(() => {
    fetch(`http://localhost:3000/api/users/${id}`, {
      credentials: "include",
    })
      .then((response) => response.json())
      .then((data) => {
        setUser(data);
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
        <div className="flex p-3">
          <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{user?.display_name}</h1>
        </div>
      </div>
    </div>
  );
};

export default Channel;
