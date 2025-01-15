import { UserRepository } from "../user/user.repository";
import { AuthHandler } from "./auth.handler";
import { AuthService } from "./auth.service";

export type AuthModule = {
    service: AuthService;
    handler: AuthHandler;
};

export const authModule = (userRepository: UserRepository): AuthModule => {
    const service = new AuthService(userRepository);
    const handler = new AuthHandler();

    return {
        service,
        handler,
    };
};
