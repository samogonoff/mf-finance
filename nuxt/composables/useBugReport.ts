/**
 * Сбор контекста и отправка пользовательских баг-репортов.
 * Эталон: mp/nuxt/composables/useBugReport.ts.
 *
 * Логи консоли, JS-ошибки и сетевой журнал копит плагин
 * plugins/bug-tracker-capture.client.ts ($bugTracker); здесь они только
 * забираются в момент отправки.
 *
 * Скриншот — через динамический import html2canvas (загружается только
 * при открытии формы, чтобы не раздувать бандл).
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

// Разделы кабинета = первый сегмент пути; должен совпадать с allowedSections в Go.
const KNOWN_SECTIONS = ["finance", "cost", "operations", "reports", "plans", "counterparties", "analytics", "account", "admin"];

export const detectSection = (path: string): string => {
  if (!path) return "other";
  const seg = path.split("/").filter(Boolean)[0] || "";
  return KNOWN_SECTIONS.includes(seg) ? seg : "other";
};

export const useBugReport = () => {
  const config = useRuntimeConfig();
  const base = config.public.apiBase;
  const route = useRoute();
  const nuxtApp = useNuxtApp();
  const tracker = () => (nuxtApp as any).$bugTracker;

  const collectRoute = () => ({
    path: route.path,
    fullPath: route.fullPath,
    name: route.name ? String(route.name) : null,
    params: { ...route.params },
    query: { ...route.query },
    url: process.client ? window.location.href : ""
  });

  const collectTechContext = (): Record<string, any> => {
    if (!process.client) return {};
    return {
      userAgent: navigator.userAgent,
      platform: (navigator as any).userAgentData?.platform || navigator.platform || "",
      language: navigator.language,
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
      timezoneOffsetMin: new Date().getTimezoneOffset(),
      screen: { w: window.screen.width, h: window.screen.height, dpr: window.devicePixelRatio || 1 },
      viewport: { w: window.innerWidth, h: window.innerHeight },
      theme: document.documentElement.dataset.theme || null,
      sessionId: tracker()?.getSessionId?.() ?? null,
      timestamp: new Date().toISOString()
    };
  };

  /** Что уйдёт в репорт из буфера перехватчика — для сводки в форме. */
  const capturedStats = () => {
    const net: any[] = tracker()?.getNetworkLog?.() ?? [];
    return {
      console: tracker()?.getConsoleLogs?.()?.length ?? 0,
      network: net.length,
      networkErrors: net.filter((e) => !e.ok).length,
      js: tracker()?.getJsErrors?.()?.length ?? 0
    };
  };

  /**
   * Снимок видимой области. Звать ДО открытия формы: иначе html2canvas снимет
   * открытый drawer и затемнение вместо страницы. FAB и drawer дополнительно
   * помечены data-html2canvas-ignore.
   */
  const captureScreenshot = async (): Promise<Blob | null> => {
    if (!process.client) return null;
    try {
      // @ts-ignore — без типов, динамический import чтобы не раздувать main bundle
      const mod = await import("html2canvas").catch(() => null);
      if (!mod) return null;
      const html2canvas: any = (mod as any).default || mod;
      const canvas = await html2canvas(document.body, {
        useCORS: true,
        logging: false,
        // scale=1 — не раздуваем PNG в 4–8 раз на HiDPI.
        scale: 1,
        width: window.innerWidth,
        height: window.innerHeight,
        x: window.scrollX,
        y: window.scrollY,
        ignoreElements: (el: Element) => el instanceof HTMLElement && el.hasAttribute("data-html2canvas-ignore")
      });
      return await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/png", 0.9));
    } catch (err) {
      console.warn("[BugTracker] screenshot failed:", err);
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
      url: process.client ? window.location.href : "",
      route: ctx.route ?? collectRoute(),
      entity_ref: ctx.entityRef ?? {},
      context_snapshot: ctx.contextSnapshot ?? {},
      tech_context: ctx.techContext ?? collectTechContext(),
      console_logs: ctx.consoleLogs ?? tracker()?.getConsoleLogs?.() ?? [],
      network_errors: ctx.networkErrors ?? tracker()?.getNetworkLog?.() ?? [],
      js_errors: ctx.jsErrors ?? tracker()?.getJsErrors?.() ?? []
    };

    const form = new FormData();
    form.append("payload", JSON.stringify(payload));
    (args.screenshots ?? []).forEach((blob, i) => {
      const ext = blob.type === "image/jpeg" ? "jpg" : "png";
      form.append("screenshots", blob, `screenshot-${i + 1}.${ext}`);
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

  return { collectRoute, collectTechContext, capturedStats, captureScreenshot, submit };
};
