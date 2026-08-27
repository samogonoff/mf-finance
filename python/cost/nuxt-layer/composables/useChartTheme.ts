/**
 * Цвета графиков Chart.js из токенов дизайн-системы.
 *
 * Хардкодить цвета запрещено (см. nuxt/assets/styles/design-system.css) —
 * берём токены из :root в рантайме. Канвас не понимает CSS-переменные и
 * color-mix(): такие значения молча игнорируются, серия рисуется чёрной или
 * исчезает, поэтому альфа-варианты считаем сами из hex.
 *
 * Тему кабинета переключают на ходу, а токены прочитаны один раз — без
 * наблюдателя графики остались бы в светлых цветах на тёмном фоне. Следим за
 * атрибутами <html>, каким бы способом тема ни переключалась.
 *
 * Категориальной палитры в дизайн-системе НЕТ (только accent/info/pos/neg):
 * для многих серий строим рамп из этих четырёх с прозрачностями. Появится
 * палитра — менять здесь, в одном месте.
 */
import { onBeforeUnmount, onMounted, ref } from "vue";

/** #rrggbb → rgba(). Не-hex возвращается как есть. */
export function withAlpha(hex: string, alpha: number): string {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
  if (!m) return hex;
  const n = parseInt(m[1], 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`;
}

export const useChartTheme = () => {
  const palette = ref<string[]>([]);
  const ink = ref("#6b7280");
  const grid = ref("rgba(127,127,127,.2)");
  const accent = ref("#4338ca");
  const info = ref("#0b5cad");
  const pos = ref("#0a7f3f");
  const neg = ref("#b42318");

  function readTokens() {
    if (typeof window === "undefined") return;
    const cs = getComputedStyle(document.documentElement);
    const tok = (n: string, fb: string) => (cs.getPropertyValue(n) || "").trim() || fb;
    accent.value = tok("--accent", "#4338ca");
    info.value = tok("--info", "#0b5cad");
    pos.value = tok("--pos", "#0a7f3f");
    neg.value = tok("--neg", "#b42318");
    ink.value = tok("--text-muted", "#6b7280");
    grid.value = withAlpha(tok("--border", "#e3e6ea"), 0.9);
    const base = [accent.value, info.value, pos.value, neg.value];
    palette.value = [
      ...base,
      ...base.map((c) => withAlpha(c, 0.6)),
      ...base.map((c) => withAlpha(c, 0.35)),
    ];
  }

  let observer: MutationObserver | null = null;
  onMounted(() => {
    readTokens();
    observer = new MutationObserver(readTokens);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "data-theme", "style"],
    });
  });
  onBeforeUnmount(() => observer?.disconnect());

  const color = (i: number) => palette.value[i % (palette.value.length || 1)] || "#888";

  /** Курсор-указатель поверх канваса: «кликабельность» иначе никак не видна —
   * Chart.js рисует в канвасе, а не в DOM. */
  function pointerOnHover(e: any, elements: any[]) {
    const canvas = e?.native?.target;
    if (canvas) canvas.style.cursor = elements.length ? "pointer" : "default";
  }

  return { palette, ink, grid, accent, info, pos, neg, color, withAlpha, pointerOnHover };
};
