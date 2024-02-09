import { TwitchToken, TwitchUserData } from "./twitchModel";

export interface UserSession {
    twitchToken: TwitchToken;
    twitchUserID: string;
    twitchUserData: TwitchUserData;
}
