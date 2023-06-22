import express, { Request, Response, NextFunction } from "express";
import session from "express-session";
import passport from "passport";
import path from "path";
import dotenv from "dotenv";
import cors from "cors";
import routes from "./routes";
import moment from "moment-timezone";
import morgan from "morgan";
import errorHandler from "./middlewares/errorHandler";
import { CustomError } from "./types/types";
import { logger, errorLogger, requestLogger } from "./middlewares/loggerMiddleware";
import { dbMiddleware } from "./middlewares/dbMiddleware";

dotenv.config({ path: path.resolve(__dirname, "../.env") });

import "./middlewares/passport";

const app = express();

const PORT: number = 8080;
const HOST: string = "0.0.0.0";

const SESSION_SECRET = process.env.SESSION_SECRET;
const REACT_URL = process.env.REACT_URL;
moment.tz.setDefault("Europe/Paris");

if (!SESSION_SECRET) {
  console.error("No session secret provided. Shutting down...");
  process.exit(1);
}

if (process.env.NODE_ENV === "development") {
  app.use(morgan("dev"));
}

app.use(
  cors({
    origin: REACT_URL,
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
app.use(express.json());
app.use(dbMiddleware);
// app.use(express.static("public"));
app.use(passport.initialize());
app.use(passport.session());
app.use(requestLogger);
app.use("/api", routes);
app.use(errorLogger);
app.use((err: CustomError, req: Request, res: Response, next: NextFunction) => {
  // console.error(err.stack);
  res.status(err.status || 500).json({
    message: err.message + "lol" || "An internal server error occurred.",
  });
});

app.use(errorHandler);
app.listen(PORT, () => {
  logger.info(`Running on Port: ${PORT}`);
});

export default app;
