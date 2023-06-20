import passport from "passport";
import { Strategy as OAuth2Strategy } from "passport-oauth2";
import axios from "axios";

const TWITCH_CLIENT_ID = process.env.TWITCH_CLIENT_ID;
const TWITCH_SECRET = process.env.TWITCH_SECRET;
const CALLBACK_URL = process.env.CALLBACK_URL;
const WHITELISTED_USER_ID = process.env.WHITELISTED_USER_ID || "";

OAuth2Strategy.prototype.userProfile = function (accessToken: string, done: Function) {
  axios({
    url: "https://api.twitch.tv/helix/users",
    method: "GET",
    headers: {
      "Client-ID": TWITCH_CLIENT_ID,
      Accept: "application/vnd.twitchtv.v5+json",
      Authorization: "Bearer " + accessToken,
    },
  })
    .then((response) => {
      if (response.status === 200) {
        done(null, response.data);
      } else {
        done(response.data);
      }
    })
    .catch((err) => {
      done(err);
    });
};

passport.serializeUser((user: any, done: any) => {
  done(null, user);
});

passport.deserializeUser((user: any, done: any) => {
  done(null, user);
});

passport.use(
  "twitch",
  new OAuth2Strategy(
    {
      authorizationURL: "https://id.twitch.tv/oauth2/authorize",
      tokenURL: "https://id.twitch.tv/oauth2/token",
      clientID: TWITCH_CLIENT_ID || "",
      clientSecret: TWITCH_SECRET || "",
      callbackURL: CALLBACK_URL || "",
      state: true,
    },
    (accessToken: string, refreshToken: string, profile: any, done: any) => {
      profile.accessToken = accessToken;
      profile.refreshToken = refreshToken;
      profile.twitchId = profile.data[0].id;

      if (!WHITELISTED_USER_ID.includes(profile.twitchId)) {
        done(null, null);
      } else {
        done(null, profile);
      }
    }
  )
);
