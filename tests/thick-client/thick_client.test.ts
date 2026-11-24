import { SedThickHostAdapter, controlHeightForMode } from "../../clients/thick-adapter/src/index";

export const thickClientTestSuiteReady =
  typeof SedThickHostAdapter === "function" &&
  controlHeightForMode("thick") === 32;
