/**
 * Перехватчик ошибок и сетевых запросов для баг-трекера.
 * Эталон: mp/nuxt/plugins/bug-tracker-capture.client.ts.
 *
 * Накапливает буфер в памяти (useState), который FAB «Сообщить о проблеме»
 * читает в момент отправки:
 * - window.onerror, unhandledrejection → jsErrors
 * - console.error, console.warn → consoleLogs
 * - patch window.fetch → networkLog: история ВСЕХ /api/* запросов;
 *   для status >= 400 или fetch error дополнительно сохраняются request/response body
 *   (обрезаются до 5 КБ каждый, чтобы не раздувать репорт).
 *
 * Буфер ограничен 50 событиями (FIFO) — столько же оставляет и Go (maxLogEntries).
 * $fetch (ofetch) под капотом использует native fetch, поэтому ловим всё на уровне fetch.
 * X-Request-Id из ответа Go-API сохраняется — по нему запрос находится в ELK.
 */
const MAX_BUFFER = 50;
const MAX_BODY_BYTES = 5000;

interface JsError {
  message: string;
  stack?: string;
  source?: string;
  line?: number;
  col?: number;
  ts: number;
}
interface ConsoleEntry {
  level: "error" | "warn";
  args: string[];
  ts: number;
}
interface NetEntry {
  url: string;
  method: string;
  status: number | null;
  ok: boolean;
  durationMs: number;
  ts: number;
  requestId?: string;
  errorMessage?: string;
  requestBody?: string;
  responseBody?: string;
  responseContentType?: string;
}

function pushCapped<T>(arr: T[], item: T) {
  arr.push(item);
  if (arr.length > MAX_BUFFER) arr.splice(0, arr.length - MAX_BUFFER);
}

function isApiUrl(url: string): boolean {
  return url.includes("/api/");
}

function describeRequestBody(body: BodyInit | null | undefined): string | undefined {
  if (body === null || body === undefined) return undefined;
  if (typeof body === "string") return body.slice(0, MAX_BODY_BYTES);
  if (typeof FormData !== "undefined" && body instanceof FormData) {
    const parts: string[] = [];
    try {
      for (const [k, v] of (body as any).entries()) {
        if (v instanceof File) {
          parts.push(`${k}=[File ${v.name} ${v.size}B ${v.type}]`);
        } else {
          parts.push(`${k}=${String(v).slice(0, 200)}`);
        }
        if (parts.join("&").length >= MAX_BODY_BYTES) break;
      }
    } catch { /* ignore */ }
    return "[FormData] " + parts.join("&").slice(0, MAX_BODY_BYTES);
  }
  if (typeof URLSearchParams !== "undefined" && body instanceof URLSearchParams) {
    return body.toString().slice(0, MAX_BODY_BYTES);
  }
  if (typeof Blob !== "undefined" && body instanceof Blob) {
    return `[Blob size=${body.size} type=${body.type || "?"}]`;
  }
  if (typeof ArrayBuffer !== "undefined" && body instanceof ArrayBuffer) {
    return `[ArrayBuffer size=${body.byteLength}]`;
  }
  return `[${Object.prototype.toString.call(body)}]`;
}

async function safeReadResponseBody(res: Response): Promise<{ body?: string; contentType?: string }> {
  const contentType = res.headers.get("content-type") || undefined;
  try {
    const clone = res.clone();
    const text = await clone.text();
    return { body: text.slice(0, MAX_BODY_BYTES), contentType };
  } catch {
    return { contentType };
  }
}

export default defineNuxtPlugin(() => {
  if (!process.client) return;

  const jsErrors = useState<JsError[]>("bt:jsErrors", () => []);
  const consoleLogs = useState<ConsoleEntry[]>("bt:consoleLogs", () => []);
  const networkLog = useState<NetEntry[]>("bt:networkLog", () => []);
  const sessionId = useState<string>("bt:sessionId", () => {
    try {
      const k = "bt_session_id";
      const existing = sessionStorage.getItem(k);
      if (existing) return existing;
      const id = "s_" + Math.random().toString(36).slice(2) + Date.now().toString(36);
      sessionStorage.setItem(k, id);
      return id;
    } catch {
      return "s_" + Date.now().toString(36);
    }
  });

  // 1. window.onerror
  window.addEventListener("error", (event: ErrorEvent) => {
    pushCapped(jsErrors.value, {
      message: String(event.message || event.error?.message || "Unknown error"),
      stack: event.error?.stack ? String(event.error.stack).slice(0, 4000) : undefined,
      source: event.filename || undefined,
      line: event.lineno,
      col: event.colno,
      ts: Date.now(),
    });
  });

  // 2. unhandledrejection
  window.addEventListener("unhandledrejection", (event: PromiseRejectionEvent) => {
    const reason: any = event.reason;
    pushCapped(jsErrors.value, {
      message: "Unhandled rejection: " + String(reason?.message || reason).slice(0, 500),
      stack: reason?.stack ? String(reason.stack).slice(0, 4000) : undefined,
      ts: Date.now(),
    });
  });

  // 3. console.error / console.warn
  const origError = console.error.bind(console);
  const origWarn = console.warn.bind(console);
  console.error = (...args: unknown[]) => {
    try {
      pushCapped(consoleLogs.value, {
        level: "error",
        args: args.map(stringifyArg).slice(0, 8),
        ts: Date.now(),
      });
    } catch { /* ignore */ }
    origError(...args);
  };
  console.warn = (...args: unknown[]) => {
    try {
      pushCapped(consoleLogs.value, {
        level: "warn",
        args: args.map(stringifyArg).slice(0, 8),
        ts: Date.now(),
      });
    } catch { /* ignore */ }
    origWarn(...args);
  };

  // 4. patch window.fetch — ловим ВСЕ /api/* запросы.
  // $fetch (ofetch) под капотом использует window.fetch, так что отдельный патч $fetch не нужен.
  const nativeFetch = window.fetch.bind(window);
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url =
      typeof input === "string"
        ? input
        : input instanceof URL
          ? input.toString()
          : (input as Request).url || String(input);

    // Не логируем не-api (статика, иконки и пр.) — иначе буфер замусорится.
    if (!isApiUrl(url)) {
      return nativeFetch(input, init);
    }

    const startedAt = performance.now();
    const method = String(
      init?.method || (typeof input !== "string" && (input as Request).method) || "GET",
    ).toUpperCase();
    const requestBody = describeRequestBody(init?.body);

    try {
      const res = await nativeFetch(input, init);
      const durationMs = Math.round(performance.now() - startedAt);
      const requestId = res.headers.get("x-request-id") || undefined;
      const entry: NetEntry = {
        url: url.slice(0, 1024),
        method,
        status: res.status,
        ok: res.ok,
        durationMs,
        ts: Date.now(),
        requestId,
      };
      if (!res.ok) {
        const { body, contentType } = await safeReadResponseBody(res);
        entry.responseBody = body;
        entry.responseContentType = contentType;
        entry.requestBody = requestBody;
      }
      pushCapped(networkLog.value, entry);
      return res;
    } catch (err: any) {
      const durationMs = Math.round(performance.now() - startedAt);
      pushCapped(networkLog.value, {
        url: url.slice(0, 1024),
        method,
        status: null,
        ok: false,
        durationMs,
        ts: Date.now(),
        errorMessage: String(err?.message || err).slice(0, 500),
        requestBody,
      });
      throw err;
    }
  };

  // Подсаживаем накопитель в провайды
  return {
    provide: {
      bugTracker: {
        getJsErrors: () => [...jsErrors.value],
        getConsoleLogs: () => [...consoleLogs.value],
        getNetworkLog: () => [...networkLog.value],
        getSessionId: () => sessionId.value,
        clear: () => {
          jsErrors.value = [];
          consoleLogs.value = [];
          networkLog.value = [];
        },
      },
    },
  };
});

function stringifyArg(v: unknown): string {
  if (v === null || v === undefined) return String(v);
  if (typeof v === "string") return v.slice(0, 500);
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  if (v instanceof Error) return `${v.name}: ${v.message}`;
  try {
    return JSON.stringify(v).slice(0, 500);
  } catch {
    return Object.prototype.toString.call(v);
  }
}
