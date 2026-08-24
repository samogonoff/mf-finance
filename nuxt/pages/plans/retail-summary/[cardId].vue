<!--
  Экран согласования формы «Розница» (ТЗ Розница §5 и §8).
  Ввода здесь нет вообще: согласующий читает агрегаты, проверяет контрольные
  сверки и принимает решение в панели процесса внизу.

  Два требования определяют весь макет.

  1) §5.0 — экран открывается СВЁРНУТЫМ. На согласовании нужен уровень разрезов
     («Розница BY», в т.ч. LFL / до года / новые / закрыты), а не 375 строк;
     магазины раскрываются по клику, «Раскрыть всё» существует, но не по
     умолчанию.

  2) §7 — контрольные сверки стоят ВЫШЕ чисел, а не в подвале. Согласующий
     обязан увидеть, что свод не сходится, до того как нажмёт «Согласовать»;
     пропущенная сверка (нет источника) показывается серым и отдельно от нуля —
     «не смогли проверить» это не «расхождение 0».
-->
<template>
  <div class="page-rsum">
    <header class="page-header">
      <div>
        <h1 class="page-title">{{ sum?.title || card?.title || "Свод розницы" }} · {{ periodLabel }}</h1>
        <p class="page-subtitle">
          Экран согласования TPL-TO-RETAIL
          <span v-if="sum"> · {{ sum.rows }} магазинов · показатели месяца {{ periodLabel }}</span>
          <span v-if="sum"> · нац. валюта {{ sum.nat_currency }}</span>
        </p>
      </div>
      <div class="page-actions">
        <NuxtLink v-if="card" :to="`/plans/${card.pl_id}`" class="btn btn-sm btn-ghost">
          <Icon name="lucide:arrow-left" /> К периоду
        </NuxtLink>
        <NuxtLink :to="`/plans/retail-form/${cardId}`" class="btn btn-sm btn-ghost">
          <Icon name="lucide:file-input" /> Форма ввода
        </NuxtLink>
        <button class="btn btn-sm btn-ghost" :disabled="loading" @click="load">
          <Icon name="lucide:refresh-cw" :class="{ spin: loading }" /> Обновить
        </button>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>

    <SkeletonTable v-if="loading && !sum" :rows="8" :cols="10" />

    <template v-if="sum">
      <!-- ===== Контрольные сверки (§7) ===== -->
      <section class="card recon-card" :class="{ 'recon-bad': hasBadRecon }">
        <div class="card-header">
          <span class="card-title">Контрольные сверки</span>
          <span class="badge" :class="reconBadge.cls">{{ reconBadge.text }}</span>
        </div>
        <div class="recon-list">
          <div v-for="r in sum.reconciliations" :key="r.code" class="rc" :class="reconClass(r)">
            <div class="rc-head">
              <span class="rc-code">{{ r.code }}</span>
              <span class="rc-title">{{ r.title }}</span>
              <span class="rc-diff">
                <template v-if="r.skipped">не выполнена</template>
                <template v-else>Δ {{ money(r.diff) }}</template>
              </span>
              <button
                v-if="!r.skipped && !r.ok && r.details?.length"
                type="button"
                class="link-btn"
                @click="toggleRecon(r.code)"
              >{{ openRecon.has(r.code) ? "скрыть расшифровку" : `расшифровка (${r.details.length})` }}</button>
            </div>
            <p v-if="r.skipped" class="rc-note">
              {{ r.note || "Источник недоступен — проверить сверку нельзя. Это НЕ «расхождение 0»." }}
            </p>
            <p v-else-if="!r.ok" class="rc-note">
              Свод не сходится: слева {{ money(r.left) }}, справа {{ money(r.right) }}.
              Согласовывать с ненулевой сверкой нельзя — расхождение надо объяснить или исправить в форме.
            </p>
            <table v-if="openRecon.has(r.code) && r.details?.length" class="data-table compact rc-tbl">
              <thead><tr><th>Магазин</th><th class="col-num">Слева</th><th class="col-num">Справа</th><th class="col-num">Δ</th><th>Пометка</th></tr></thead>
              <tbody>
                <tr v-for="(d, i) in r.details" :key="i">
                  <td>{{ d.cfo || "—" }} <span class="rc-cfo">ЦФО {{ d.code_cfo }}</span></td>
                  <td class="col-num">{{ money(d.left) }}</td>
                  <td class="col-num">{{ money(d.right) }}</td>
                  <td class="col-num delta-neg">{{ money(d.diff) }}</td>
                  <td class="rc-cfo">{{ d.note || "" }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </section>

      <!-- ===== Блокирующие замечания формы (§6) — коротко, для контекста ===== -->
      <section v-if="report && (report.blocking.length || !report.can_submit)" class="card">
        <div class="card-header">
          <span class="card-title">Замечания формы</span>
          <span class="badge badge-neg">блокирующих: {{ report.blocking.length }}</span>
          <span v-if="report.warnings.length" class="badge badge-warn">предупреждений: {{ report.warnings.length }}</span>
        </div>
        <ul class="issue-ul">
          <li v-for="(i, k) in report.blocking.slice(0, 12)" :key="k">
            <span class="badge badge-neg">{{ i.code }}</span> {{ i.message }}
          </li>
        </ul>
        <p v-if="report.blocking.length > 12" class="issue-more">
          …и ещё {{ report.blocking.length - 12 }}. Полный список — на форме ввода, кнопка «Проверить».
        </p>
      </section>

      <!-- ===== Разрезы и показатели (§8) ===== -->
      <section class="card">
        <div class="card-header">
          <span class="card-title">Показатели месяца</span>
          <div class="sum-tools">
            <select v-model="dim" class="select">
              <option value="lfl">разрез: LFL-статусы (§5.1)</option>
              <option v-for="g in sum.groups" :key="g.key" :value="g.key">разрез: {{ g.title }}</option>
            </select>
            <button type="button" class="btn btn-sm btn-ghost" @click="toggleAll">
              <Icon :name="allOpen ? 'lucide:chevrons-down-up' : 'lucide:chevrons-up-down'" />
              {{ allOpen ? "Свернуть всё" : "Раскрыть всё" }}
            </button>
          </div>
        </div>

        <div v-for="b in sum.blocks" :key="b.currency" class="cur-block">
          <div class="cb-head">
            <span class="cb-cur">{{ b.currency }}</span>
            <span class="cb-kind">{{ b.full ? "полный набор показателей" : "сокращённый набор" }}</span>
            <span v-if="b.fx_rate !== 1" class="cb-fx">курс к {{ sum.nat_currency }}: {{ rate(b.fx_rate) }}</span>
          </div>
          <div class="cb-scroll">
            <table class="data-table compact">
              <thead>
                <tr>
                  <th class="s-col">Разрез</th>
                  <th class="col-num">Магазинов</th>
                  <th v-for="f in b.fields" :key="f" class="col-num" :title="FIELD_HINTS[f]">{{ FIELD_LABELS[f] || f }}</th>
                </tr>
              </thead>
              <tbody>
                <!-- Итог формы — первой строкой: с него согласующий начинает чтение. -->
                <tr class="row-total">
                  <td class="s-col">{{ sum.title }}</td>
                  <td class="col-num">{{ int(sum.rows) }}</td>
                  <td v-for="f in b.fields" :key="f" class="col-num">{{ cellText(b.total, f) }}</td>
                </tr>
                <template v-for="s in sectionsOf(b)" :key="s.key">
                  <tr class="row-sec" :class="{ 'is-open': open.has(s.key) }" @click="toggle(s.key)">
                    <td class="s-col">
                      <Icon :name="open.has(s.key) ? 'lucide:chevron-down' : 'lucide:chevron-right'" class="s-caret" />
                      {{ s.title }}
                    </td>
                    <td class="col-num">{{ int(s.rows) }}</td>
                    <td v-for="f in b.fields" :key="f" class="col-num">{{ cellText(s.metrics, f) }}</td>
                  </tr>
                  <tr v-for="st in open.has(s.key) ? storesOf(s.key) : []" :key="s.key + ':' + st.code_cfo" class="row-store">
                    <td class="s-col">
                      <span class="st-name">{{ st.cfo || "—" }}</span>
                      <span class="st-meta">ЦФО {{ st.code_cfo }} · {{ st.city }}</span>
                    </td>
                    <td class="col-num">1</td>
                    <td v-for="f in b.fields" :key="f" class="col-num">{{ cellText(scaleMetrics(st.metrics, b.fx_rate), f) }}</td>
                  </tr>
                  <tr v-if="open.has(s.key) && !storesOf(s.key).length" class="row-store">
                    <td :colspan="b.fields.length + 2" class="st-empty">
                      В этом разрезе нет строк формы (или форма ввода недоступна вашему срезу).
                    </td>
                  </tr>
                </template>
              </tbody>
            </table>
          </div>
        </div>
        <p class="sum-note">
          Денежные показатели пересчитаны в валюту блока по курсу; проценты — отношения и от валюты
          не зависят, поэтому в блоках BYN и USD они те же числа, что и в национальном.
        </p>
      </section>

      <!-- Лист согласования, версии, публикация (§5, §8). -->
      <CardProcessPanel
        :card-id="cardId"
        :can-submit="report ? report.can_submit : true"
        :submit-hint="submitHint"
        @changed="load"
      />
    </template>
  </div>
</template>

<script setup lang="ts">
import CardProcessPanel from "~/components/plans/CardProcessPanel.vue";
import SkeletonTable from "~/components/SkeletonTable.vue";
import { useMpConditions, type PlanCard } from "~/composables/useMpConditions";
import {
  iferrorDiv, iferrorRatioMinus1, retailErrText, useRetail,
  type RetailCurrencyBlock, type RetailForm, type RetailReconciliation,
  type RetailSummary, type RetailSummaryMetrics, type RetailSummarySection,
  type RetailValidationReport
} from "~/composables/useRetail";
import { num as fmtNum } from "~/utils/format";

definePageMeta({ middleware: ["scope-guard"] });

const route = useRoute();
const cardId = Number(route.params.cardId);
const api = useRetail();
const cardApi = useMpConditions();

const sum = ref<RetailSummary | null>(null);
const form = ref<RetailForm | null>(null);
const card = ref<PlanCard | null>(null);
const report = ref<RetailValidationReport | null>(null);
const loading = ref(true);
const error = ref("");

/** Текущий разрез: "lfl" — разрезы §5.1, иначе ключ группировки из ответа. */
const dim = ref("lfl");
/** Раскрытые разрезы. Пусто = свёрнутый вид по умолчанию (§5.0). */
const open = reactive(new Set<string>());
const openRecon = reactive(new Set<string>());

const periodLabel = computed(() =>
  sum.value ? `${String(sum.value.month).padStart(2, "0")}.${sum.value.year}` : ""
);

// ─────────── подписи показателей (§8) ───────────
const FIELD_LABELS: Record<string, string> = {
  fact_prev_year: "Факт ПГ",
  strategy: "Стратегия",
  tactic: "Тактика",
  tactic_vs_strategy: "Такт./страт.",
  tactic_vs_prev_month: "Такт./пред.мес",
  fact_cur: "Факт",
  lfl: "LFL",
  tactic_vs_fact_py: "Такт./факт ПГ",
  fact_vs_fact_py: "Факт/факт ПГ",
  done_vs_strategy: "% вып. страт.",
  done_vs_tactic: "% вып. такт.",
  period_total: "Итого период",
  year_expectation: "Ожидание года",
  fact_prev_month: "Факт пред.мес"
};
const FIELD_HINTS: Record<string, string> = {
  fact_prev_year: "Факт того же месяца прошлого года",
  tactic_vs_strategy: "Тактика / стратегия",
  tactic_vs_prev_month: "Тактика / факт предыдущего месяца",
  lfl: "Тактика / факт того же месяца ПГ − 1",
  done_vs_strategy: "Факт / стратегия",
  done_vs_tactic: "Факт / тактика",
  year_expectation: "Факт закрытых месяцев + тактика остальных"
};

/** Денежные поля — их и только их пересчитывает курс (§4: проценты не пересчитываются). */
const MONEY_FIELDS = new Set(["fact_prev_year", "strategy", "tactic", "fact_cur", "period_total", "year_expectation", "fact_prev_month"]);
/** Поля-отклонения: показываем со знаком, остальные проценты — как долю выполнения. */
const DELTA_FIELDS = new Set(["lfl"]);

const int = (v: number) => fmtNum(v, 0);
const money = (v: number) => fmtNum(v, 0);
const rate = (v: number) => fmtNum(v, 4);

const cellText = (m: RetailSummaryMetrics, f: string): string => {
  const v = m?.[f];
  if (v === undefined || v === null) return "—";
  if (MONEY_FIELDS.has(f)) return money(v);
  const p = v * 100;
  const s = p.toLocaleString("ru-RU", { minimumFractionDigits: 1, maximumFractionDigits: 1 });
  return DELTA_FIELDS.has(f) ? `${p > 0 ? "+" : ""}${s} %` : `${s} %`;
};

/** Пересчёт агрегата строки-магазина в валюту блока — тем же правилом, что на сервере. */
const scaleMetrics = (m: RetailSummaryMetrics, r: number): RetailSummaryMetrics => {
  if (!r || r === 1) return m;
  const out: RetailSummaryMetrics = { ...m };
  for (const f of MONEY_FIELDS) if (typeof out[f] === "number") out[f] = out[f] * r;
  return out;
};

// ─────────── разрезы и раскрытие до магазина ───────────
const sectionsOf = (b: RetailCurrencyBlock): RetailSummarySection[] => {
  if (dim.value === "lfl") return b.sections;
  const g = sum.value?.groups.find((x) => x.key === dim.value);
  if (!g) return b.sections;
  // Группировки сервер считает только в нац. валюте — для блоков BYN/USD
  // пересчитываем сами тем же правилом, иначе разрез «по городу» пришлось бы
  // прятать в валютных блоках, а он там нужен ровно так же.
  return g.sections.map((s) => ({ ...s, metrics: scaleMetrics(s.metrics, b.fx_rate) }));
};

/** Предикат «строка формы принадлежит разрезу» — зеркало retailSectionOrder на сервере. */
const inSection = (r: RetailForm["rows"][number], key: string): boolean => {
  if (dim.value !== "lfl") {
    return String((r as unknown as Record<string, unknown>)[dim.value] ?? "") === key;
  }
  switch (key) {
    case "lfl": return r.lfl_effective === "lfl";
    case "under1y": return r.lfl_effective === "under1y";
    case "new": return r.lfl_effective === "new" || r.lfl_effective === "xxx";
    case "closed": return r.lfl_effective === "closed" || !!r.date_close;
    default: return false;
  }
};

interface StoreLine { code_cfo: number; cfo: string; city: string; metrics: RetailSummaryMetrics }

/**
 * Строки магазинов разреза. Свод сервера отдаёт только количество строк, поэтому
 * детализацию собираем из формы теми же формулами, что и aggregateMetrics: они
 * состоят из сложения индикаторов строки и отношений на суммах, для одной строки
 * это ровно её собственные показатели.
 */
const storesOf = (key: string): StoreLine[] => {
  const rows = form.value?.rows || [];
  return rows
    .filter((r) => inSection(r, key))
    .map((r) => {
      const i = r.indicators;
      const fpy = i.fact_prev_year_month ?? 0;
      const fpm = i.fact_prev_month ?? 0;
      const fcur = i.fact_cur_month ?? 0;
      const strat = i.strategy ?? 0;
      const tact = i.tactic ?? 0;
      return {
        code_cfo: r.code_cfo,
        cfo: r.cfo,
        city: r.city,
        metrics: {
          fact_prev_year: fpy,
          strategy: strat,
          tactic: tact,
          tactic_vs_strategy: iferrorDiv(tact, strat),
          tactic_vs_prev_month: iferrorDiv(tact, fpm),
          fact_cur: fcur,
          lfl: iferrorRatioMinus1(tact, fpy),
          tactic_vs_fact_py: iferrorDiv(tact, fpy),
          fact_vs_fact_py: iferrorDiv(fcur, fpy),
          done_vs_strategy: iferrorDiv(fcur, strat),
          done_vs_tactic: iferrorDiv(fcur, tact),
          period_total: i.period_total,
          year_expectation: i.year_expectation,
          fact_prev_month: fpm
        }
      };
    })
    .sort((a, b) => a.city.localeCompare(b.city, "ru") || a.code_cfo - b.code_cfo);
};

const allKeys = computed<string[]>(() => {
  const b = sum.value?.blocks?.[0];
  if (!b) return [];
  return sectionsOf(b).map((s) => s.key);
});
const allOpen = computed(() => allKeys.value.length > 0 && allKeys.value.every((k) => open.has(k)));
const toggle = (k: string) => (open.has(k) ? open.delete(k) : open.add(k));
const toggleAll = () => {
  if (allOpen.value) open.clear();
  else allKeys.value.forEach((k) => open.add(k));
};
// Смена разреза сбрасывает раскрытие: ключи у разрезов разные, и «открытая
// Минская область» после переключения на LFL выглядела бы случайной.
watch(dim, () => open.clear());

// ─────────── сверки ───────────
const toggleRecon = (c: string) => (openRecon.has(c) ? openRecon.delete(c) : openRecon.add(c));
const reconClass = (r: RetailReconciliation) => (r.skipped ? "rc-skip" : r.ok ? "rc-ok" : "rc-bad");
const hasBadRecon = computed(() => (sum.value?.reconciliations || []).some((r) => !r.ok && !r.skipped));
const skippedRecon = computed(() => (sum.value?.reconciliations || []).filter((r) => r.skipped).length);
const reconBadge = computed(() => {
  if (hasBadRecon.value) return { cls: "badge-neg", text: "свод не сходится" };
  if (skippedRecon.value) return { cls: "badge-warn", text: `не проверено: ${skippedRecon.value}` };
  return { cls: "badge-pos", text: "все сверки сошлись" };
});

const submitHint = computed(() => {
  if (hasBadRecon.value) return "контрольные сверки не сходятся";
  if (report.value && !report.value.can_submit) return `блокирующих замечаний: ${report.value.blocking.length}`;
  return "";
});

// ─────────── загрузка ───────────
const load = async () => {
  loading.value = true;
  error.value = "";
  try {
    const [s, f, c, rep] = await Promise.all([
      api.summary(cardId),
      // Форма нужна только для раскрытия до магазина: свод сам строк не отдаёт.
      api.form(cardId).catch(() => null),
      cardApi.card(cardId).then((r) => r.card).catch(() => null),
      api.validate(cardId).catch(() => null)
    ]);
    sum.value = s;
    form.value = f;
    card.value = c;
    report.value = rep;
  } catch (e) {
    error.value = retailErrText(e, "Не удалось загрузить свод");
  } finally {
    loading.value = false;
  }
};

onMounted(load);
</script>

<style scoped>
.page-rsum {
  display: flex;
  flex-direction: column;
  gap: var(--sp-5);
}
.banner {
  margin: 0;
  padding: var(--sp-4) var(--sp-5);
  border-radius: var(--rd-4);
  font-size: var(--fs-sm);
}
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
.link-btn {
  background: none;
  border: none;
  padding: 0;
  color: var(--text-link);
  font: inherit;
  font-size: var(--fs-xs);
  cursor: pointer;
  text-decoration: underline;
}
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

/* ─── сверки ─── */
.recon-card.recon-bad { border-color: var(--neg); }
.recon-list { display: flex; flex-direction: column; }
.rc { padding: var(--sp-4) var(--sp-5); border-bottom: 1px solid var(--border); }
.rc:last-child { border-bottom: none; }
.rc-head { display: flex; align-items: baseline; gap: var(--sp-4); flex-wrap: wrap; }
.rc-code {
  font-family: var(--font-mono);
  font-size: var(--fs-xs);
  padding: 1px var(--sp-3);
  border-radius: var(--rd-3);
  background: var(--bg-surface-3);
  color: var(--text-secondary);
}
.rc-title { flex: 1 1 240px; }
.rc-diff {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-weight: var(--fw-semibold);
}
.rc-ok .rc-diff { color: var(--pos); }
.rc-bad .rc-diff { color: var(--neg); }
.rc-bad .rc-code { background: var(--neg-soft); color: var(--neg-strong); }
.rc-skip .rc-diff, .rc-skip .rc-title { color: var(--text-muted); }
.rc-skip .rc-code { background: var(--bg-surface-3); color: var(--text-muted); }
.rc-note { margin: var(--sp-2) 0 0; font-size: var(--fs-sm); color: var(--text-secondary); }
.rc-skip .rc-note { color: var(--text-muted); }
.rc-bad .rc-note { color: var(--neg); }
.rc-tbl { margin-top: var(--sp-4); }
.rc-cfo { font-size: var(--fs-xs); color: var(--text-muted); }

/* ─── замечания ─── */
.issue-ul {
  margin: 0;
  padding: var(--sp-4) var(--sp-5);
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: var(--sp-2);
  font-size: var(--fs-sm);
  color: var(--text-secondary);
}
.issue-more { margin: 0; padding: 0 var(--sp-5) var(--sp-5); font-size: var(--fs-xs); color: var(--text-muted); }

/* ─── показатели ─── */
.sum-tools { display: flex; align-items: center; gap: var(--sp-3); margin-left: auto; }
.cur-block { border-top: 1px solid var(--border); }
.cb-head {
  display: flex;
  align-items: baseline;
  gap: var(--sp-4);
  padding: var(--sp-4) var(--sp-5);
  background: var(--bg-surface-2);
}
.cb-cur {
  font-family: var(--font-mono);
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
}
.cb-kind, .cb-fx { font-size: var(--fs-xs); color: var(--text-muted); }
.cb-scroll { overflow-x: auto; }
.s-col { min-width: 260px; }
.row-total td {
  background: var(--bg-surface-3);
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
}
.row-sec { cursor: pointer; }
.row-sec td { color: var(--text-secondary); }
.row-sec.is-open td { background: var(--accent-soft); }
.s-caret { color: var(--text-muted); vertical-align: -2px; }
.row-store td { background: var(--bg-surface-2); font-size: var(--fs-sm); }
.st-name { display: block; padding-left: var(--sp-6); }
.st-meta { display: block; padding-left: var(--sp-6); font-size: var(--fs-xs); color: var(--text-muted); }
.st-empty { padding: var(--sp-5); color: var(--text-muted); }
.sum-note {
  margin: 0;
  padding: var(--sp-4) var(--sp-5);
  border-top: 1px solid var(--border);
  font-size: var(--fs-xs);
  color: var(--text-muted);
}
</style>
