export type SedClientMode = "web" | "thick" | "thin";

export type SedInboundEvent =
  | { type: "sed:ready"; clientMode: SedClientMode }
  | { type: "sed:route-changed"; path: string; clientMode: SedClientMode };

export interface SedHostOptions {
  baseUrl: string;
  token?: string;
  initialPath?: string;
  mode?: SedClientMode;
  onRouteChanged?: (path: string, mode: SedClientMode) => void;
  onReady?: (mode: SedClientMode) => void;
}

export const WVC_DESIGN_TOKENS = {
  bg: "#ffffff",
  surfaceSoft: "#f5f5f7",
  ink: "#14151f",
  ink70: "rgba(20, 21, 31, 0.7)",
  ink50: "rgba(20, 21, 31, 0.5)",
  line: "rgba(20, 21, 31, 0.12)",
  lineStrong: "rgba(20, 21, 31, 0.16)",
  lineHover: "rgba(20, 21, 31, 0.5)",
  hoverSoft: "rgba(20, 21, 31, 0.04)",
  accent: "#3339f1",
  accentSoft: "rgba(51, 57, 241, 0.12)",
  cta: "#14151f",
  ctaHover: "rgba(20, 21, 31, 0.78)",
  radiusControl: "8px",
  radiusDialog: "16px",
  backdropOverlay: "rgba(0, 0, 0, 0.2)",
  backdropBlur: "8px",
} as const;

export function assertUnreachable(value: never): never {
  throw new Error(`Unhandled SED variant: ${JSON.stringify(value)}`);
}

export function controlHeightForMode(mode: SedClientMode): number {
  switch (mode) {
    case "web":
      return 36;
    case "thick":
      return 32;
    case "thin":
      return 36;
    default:
      return assertUnreachable(mode);
  }
}

export class SedThickHostAdapter {
  private readonly opts: SedHostOptions;
  private frame: HTMLIFrameElement | null = null;
  private readonly boundListener: (ev: MessageEvent) => void;

  constructor(opts: SedHostOptions) {
    this.opts = opts;
    this.boundListener = (ev: MessageEvent) => this.handleMessage(ev);
  }

  mount(container: HTMLElement): HTMLIFrameElement {
    const mode: SedClientMode = this.opts.mode ?? "thick";
    const path = this.opts.initialPath ?? "/home";
    const base = this.opts.baseUrl.replace(/\/+$/, "");
    const iframe = document.createElement("iframe");
    iframe.src = `${base}${path}?client=${encodeURIComponent(mode)}`;
    iframe.style.width = "100%";
    iframe.style.height = "100%";
    iframe.style.border = `1px solid ${WVC_DESIGN_TOKENS.line}`;
    iframe.style.borderRadius = WVC_DESIGN_TOKENS.radiusControl;
    iframe.style.background = WVC_DESIGN_TOKENS.bg;
    iframe.title = "Orchestrator SED";

    window.addEventListener("message", this.boundListener);
    iframe.addEventListener("load", () => {
      iframe.contentWindow?.postMessage(
        {
          type: "sed:init",
          clientMode: mode,
          token: this.opts.token ?? "",
          path,
        },
        "*"
      );
    });

    container.replaceChildren(iframe);
    this.frame = iframe;
    return iframe;
  }

  navigate(path: string): void {
    this.frame?.contentWindow?.postMessage({ type: "sed:navigate", path }, "*");
  }

  openDocument(documentId: string): void {
    this.frame?.contentWindow?.postMessage({ type: "sed:open-document", documentId }, "*");
  }

  destroy(): void {
    window.removeEventListener("message", this.boundListener);
    this.frame?.remove();
    this.frame = null;
  }

  private handleMessage(ev: MessageEvent): void {
    if (!this.frame || ev.source !== this.frame.contentWindow) return;
    const data = ev.data as SedInboundEvent | undefined;
    if (!data || typeof data !== "object" || typeof data.type !== "string") return;
    if (data.type !== "sed:ready" && data.type !== "sed:route-changed") return;

    switch (data.type) {
      case "sed:ready":
        this.opts.onReady?.(data.clientMode);
        return;
      case "sed:route-changed":
        this.opts.onRouteChanged?.(data.path, data.clientMode);
        return;
      default:
        assertUnreachable(data);
    }
  }
}
