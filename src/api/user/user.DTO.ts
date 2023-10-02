import { User } from ".prisma/client";
import { SessionUser } from "../../models/userModel";

export const transformSessionUser = (user: SessionUser): User => {
    return {
        userId: user.id,
        userLogin: user.login,
        displayName: user.display_name,
        email: user.email,
        profileImageUrl: user.profile_image_url,
        createdAt: new Date(user.created_at),
    };
};
