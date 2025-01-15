import { TwitchToken, TwitchUserData } from "./model.twitch";

export interface UserSession {
    twitchToken: TwitchToken;
    twitchUserID: string;
    twitchUserData: TwitchUserData;
}
