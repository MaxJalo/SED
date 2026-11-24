import { WVC_DESIGN_TOKENS, controlHeightForMode } from "../../clients/thick-adapter/src/index";

export const webClientTestSuiteReady =
  WVC_DESIGN_TOKENS.ink === "#14151f" &&
  WVC_DESIGN_TOKENS.accent === "#3339f1" &&
  controlHeightForMode("web") === 36;
