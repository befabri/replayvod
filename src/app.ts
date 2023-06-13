import express, { Request, Response } from "express";
import session from "express-session";
import passport from "passport";
import { OAuth2Strategy } from "passport-oauth";
import request from "request";

const TWITCH_CLIENT_ID = "<YOUR CLIENT ID HERE>";
const TWITCH_SECRET = "<YOUR CLIENT SECRET HERE>";
const SESSION_SECRET = "<SOME SECRET HERE>";
const CALLBACK_URL = "<YOUR REDIRECT URL HERE>";

const app = express();

app.use(
  session({
    secret: SESSION_SECRET,
    resave: false,
    saveUninitialized: false,
  })
);
app.use(express.static("public"));
app.use(passport.initialize());
app.use(passport.session());

OAuth2Strategy.prototype.userProfile = function (
  this: any,
  accessToken: string,
  done: (error: any, profile?: any) => void
) {
  const options = {
    url: "https://api.twitch.tv/helix/users",
    method: "GET",
    headers: {
      "Client-ID": TWITCH_CLIENT_ID,
      Authorization: "Bearer " + accessToken,
    },
  };

  request(options, function (error, response, body) {
    if (response && response.statusCode == 200) {
      done(null, JSON.parse(body));
    } else {
      done(JSON.parse(body));
    }
  });
};

passport.serializeUser(function (user: any, done: (error: any, id?: any) => void) {
  done(null, user);
});

passport.deserializeUser(function (user: any, done: (error: any, user?: any) => void) {
  done(null, user);
});

passport.use(
  "twitch",
  new OAuth2Strategy(
    {
      authorizationURL: "https://id.twitch.tv/oauth2/authorize",
      tokenURL: "https://id.twitch.tv/oauth2/token",
      clientID: TWITCH_CLIENT_ID,
      clientSecret: TWITCH_SECRET,
      callbackURL: CALLBACK_URL,
      state: true,
    },
    function (
      this: any,
      accessToken: string,
      refreshToken: string,
      profile: any,
      done: (error: any, profile?: any) => void
    ) {
      profile.accessToken = accessToken;
      profile.refreshToken = refreshToken;

      // Securely store user profile in your DB
      // User.findOrCreate(..., function(err, user) {
      //   done(err, user);
      // });

      done(null, profile);
    }
  )
);

app.get("/auth/twitch", passport.authenticate("twitch", { scope: "user_read" }));

app.get("/auth/twitch/callback", passport.authenticate("twitch", { successRedirect: "/", failureRedirect: "/" }));

app.get("/", function (req: Request, res: Response) {
  if (req.session && req.session.passport && req.session.passport.user) {
    const user = req.session.passport.user;
    res.json(user);
  } else {
    res.redirect("/auth/twitch");
  }
});

const port = 3000;
app.listen(port, () => {
  console.log(`Twitch auth sample listening on port ${port}!`);
});
