// server.ts
import express from "express";
import session from "express-session";
import passport from "passport";
import path from "path";
import dotenv from "dotenv";
import cors from "cors";

import authRoutes from "./routes/auth";

dotenv.config({ path: path.resolve(__dirname, "../.env") });

import "./middlewares/passport";

const app = express();

const SESSION_SECRET = process.env.SESSION_SECRET;

if (!SESSION_SECRET) {
  console.error("No session secret provided. Shutting down...");
  process.exit(1);
}

app.use(
  cors({
    origin: "http://localhost:5173",
    credentials: true,
  })
);

app.use(
  session({
    secret: SESSION_SECRET,
    resave: false,
    saveUninitialized: true,
    cookie: { httpOnly: true },
  })
);

app.use(express.static("public"));
app.use(passport.initialize());
app.use(passport.session());

app.use("/api/auth", authRoutes);

app.listen(3000, () => {
  console.log("Listening on port 3000!");
});

export default app;
