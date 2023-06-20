import { Request, Response, NextFunction } from "express";
import { Db } from "mongodb";
import { getDbInstance } from "../models/db";

declare global {
  namespace Express {
    interface Request {
      db: Db;
    }
  }
}

export async function dbMiddleware(req: Request, res: Response, next: NextFunction) {
  if (!req.db) {
    const db = await getDbInstance();
    req.db = db;
  }
  next();
}
