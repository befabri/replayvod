import express, { Router, Request, Response, NextFunction } from "express";
import authRoutes from "./authRoutes";
import userRoutes from "./userRoutes";
import errorHandler from "../middlewares/errorHandler";
import { CustomError } from "../types/types";

const router: Router = express.Router();

router.use("/auth", authRoutes);
router.use("/users", userRoutes);

router.use((req: Request, res: Response, next: NextFunction) => {
  const error: CustomError = new Error("Not Found");
  error.status = 404;
  next(error);
});

router.use(errorHandler);

export default router;
