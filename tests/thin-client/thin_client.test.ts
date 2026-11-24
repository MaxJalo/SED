import { WVC_DESIGN_TOKENS, controlHeightForMode } from "../../clients/thick-adapter/src/index";

export const thinClientTestSuiteReady =
  WVC_DESIGN_TOKENS.radiusDialog === "16px" &&
  controlHeightForMode("thin") === 36;
