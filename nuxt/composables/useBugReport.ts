/**
 * Сбор контекста и отправка пользовательских баг-репортов.
 * Эталон: mp/nuxt/composables/useBugReport.ts.
 *
 * Скриншот — через динамический import html2canvas (загружается только
 * когда пользователь нажал «снять скриншот», чтобы не раздувать бандл).
 */

const authHeader = (): Record<string, string> => {
  if (!process.client) return {};
  const t = localStorage.getItem("auth_token");
  return t ? { Authorization: `Bearer ${t}` } : {};
};

export interface CollectedContext {
  route: Record<string, any>;
  techContext: Record<string, any>;
  consoleLogs: any[];
  networkErrors: any[];
  jsErrors: any[];
  contextSnapshot: Record<string, any>;
  entityRef: Record<string, any>;
}

export const detectSection = (path: string): string => {
  if (!path) return "other";
  const seg = path.split("/").filter(Boolean)[0] || "";
  const known = ["finance", "cost", "operations", "reports", "counterparties", "analytics", "account", "admin"];
  return known.includes(seg) ? seg : "other";
};

export const useBugReport = () => {
  const config = useRuntimeConfig();
  const base = config.public.apiBase;
  const route = useRoute();

  const collectRoute = () => ({
    path: route.path,
    fullPath: route.fullPath,
    name: route.name,
    params: { ...route.params },
    query: { ...route.query },
    url: process.client ? window.location.href : ""
  });

  const collectTechContext = (): Record<string, any> => {
    if (!process.client) return {};
    return {
      ua: navigator.userAgent,
      platform: navigator.platform,
      lang: navigator.language,
      viewport: { w: window.innerWidth, h: window.innerHeight, dpr: window.devicePixelRatio || 1 },
      cookieEnabled: navigator.cookieEnabled,
      tz: Intl.DateTimeFormat().resolvedOptions().timeZone
    };
  };

  const captureScreenshot = async (): Promise<Blob | null> => {
    if (!process.client) return null;
    try {
      // @ts-ignore — без типов, динамический import чтобы не раздувать main bundle
      const mod = await import("html2canvas").catch(() => null);
      if (!mod) return null;
      const html2canvas: any = (mod as any).default || mod;
      const canvas = await html2canvas(document.body, { useCORS: true, scale: 1, logging: false });
      return await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/png", 0.9));
    } catch {
      return null;
    }
  };

  const submit = async (
    args: {
      type: string;
      title: string;
      description: string;
      steps: string;
      screenshots?: Blob[];
      ctx?: Partial<CollectedContext>;
    }
  ) => {
    const ctx = args.ctx ?? {};
    const payload = {
      section: detectSection(route.path),
      type: args.type,
      title: args.title,
      description: args.description,
      steps: args.steps,
      route: ctx.route ?? collectRoute(),
      entity_ref: ctx.entityRef ?? {},
      context_snapshot: ctx.contextSnapshot ?? {},
      tech_context: ctx.techContext ?? collectTechContext(),
      console_logs: ctx.consoleLogs ?? [],
      network_errors: ctx.networkErrors ?? [],
      js_errors: ctx.jsErrors ?? []
    };

    const form = new FormData();
    form.append("payload", JSON.stringify(payload));
    (args.screenshots ?? []).forEach((blob, i) => {
      form.append("screenshots", blob, `screenshot-${i + 1}.png`);
    });

    return await $fetch<{ id: number; deduplicated: boolean; section: string; status: string }>(
      `${base}/api/bugtracker/report`,
      {
        method: "POST",
        body: form,
        headers: authHeader()
      }
    );
  };

  return { collectRoute, collectTechContext, captureScreenshot, submit };
};
