import { ErrorRequestHandler } from "express";
import { CustomError } from "../types/types";

const errorHandler: ErrorRequestHandler = (error: CustomError, req, res, next) => {
  const status = error.status || 500;
  const message = error.message || "An internal server error occurred.";

  res.status(status);
  res.json({
    error: {
      message,
    },
  });
};

export default errorHandler;
