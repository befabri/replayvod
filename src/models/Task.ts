import { Document, ObjectId } from "mongodb";

  export interface Task {
    _id?: ObjectId;
    id: string;
    name: string;
    description: string;
    taskType: string;
    metadata?: {
      [key: string]: string;
    };
    interval: number;
    lastExecution: Date;
    lastDuration: number;
    nextExecution: Date;
  }
