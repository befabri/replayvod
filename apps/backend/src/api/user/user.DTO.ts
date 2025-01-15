import { User } from ".prisma/client";
import { TwitchUserData } from "../../models/model.twitch";

export const transformSessionUser = (user: TwitchUserData): User => {
    return {
        userId: user.id,
        userLogin: user.login,
        displayName: user.display_name,
        email: user.email,
        profileImageUrl: user.profile_image_url,
        createdAt: new Date(user.created_at),
    };
};
