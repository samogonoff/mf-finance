/**
 * Тема: light / dark / auto. Хранится в cookie, чтобы SSR сразу отдавал
 * корректный класс на <html> без «мигания». На клиенте — реактивно
 * следим за prefers-color-scheme в режиме auto.
 */

export type Theme = "light" | "dark" | "auto";
const STORAGE_KEY = "fin.theme";

function systemPrefersDark(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) return false;
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function applyToDocument(effective: "light" | "dark"): void {
  if (typeof document === "undefined") return;
  document.documentElement.classList.toggle("theme-dark", effective === "dark");
  document.documentElement.classList.toggle("theme-light", effective === "light");
  document.body?.classList.toggle("theme-dark", effective === "dark");
  document.body?.classList.toggle("theme-light", effective === "light");
  document.documentElement.style.colorScheme = effective;
}

function normalize(value: string | null | undefined): Theme {
  return value === "light" || value === "dark" || value === "auto" ? value : "light";
}

export function useTheme() {
  const cookie = useCookie<Theme>(STORAGE_KEY, {
    default: () => "light",
    sameSite: "lax",
    path: "/",
    maxAge: 60 * 60 * 24 * 365
  });

  const theme = useState<Theme>("fin-theme", () => normalize(cookie.value));
  const systemDark = useState<boolean>("fin-theme-system-dark", () => false);

  const effective = computed<"light" | "dark">(() => {
    if (theme.value === "light") return "light";
    if (theme.value === "dark") return "dark";
    return systemDark.value ? "dark" : "light";
  });

  function setTheme(v: Theme) {
    theme.value = v;
    cookie.value = v;
  }

  function toggle() {
    setTheme(effective.value === "dark" ? "light" : "dark");
  }

  if (import.meta.client) {
    systemDark.value = systemPrefersDark();
    watch(effective, applyToDocument, { immediate: true });
    const mq = window.matchMedia?.("(prefers-color-scheme: dark)");
    const handler = (e: MediaQueryListEvent) => {
      systemDark.value = e.matches;
    };
    mq?.addEventListener?.("change", handler);
  }

  return { theme, effective, setTheme, toggle };
}
