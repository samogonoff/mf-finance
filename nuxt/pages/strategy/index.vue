<!--
  Раздел «Стратегические планы» (ТЗ доработок кабинета, п. 5.1) — на одном
  уровне с «Тактическими планами».

  ТЗ требует сам раздел; его состав пока не описан. Наполнение — то, что уже
  есть в системе и опирается на стратегию: доли статей прямых затрат площадок
  МП по месяцам из стратегического сценария. Это те же доли, что подставляются
  по умолчанию в форму тактики МП (п. 2.1), — здесь их видно на весь год сразу.
-->
<template>
  <div class="page-strategy">
    <header class="page-header">
      <div>
        <h1 class="page-title">Стратегические планы</h1>
        <p class="page-subtitle">
          Маркетплейсы · доли статей прямых затрат от продаж по цене площадки без НДС
          <span v-if="data?.source"> · источник: {{ data.source }}</span>
        </p>
      </div>
      <div class="page-actions">
        <label class="form-field inline">
          <span class="form-label">Год</span>
          <select v-model.number="year" class="select" @change="load">
            <option v-for="y in years" :key="y" :value="y">{{ y }}</option>
          </select>
        </label>
        <label class="form-field inline">
          <span class="form-label">Месяц</span>
          <select v-model.number="month" class="select">
            <option v-for="m in 12" :key="m" :value="m">{{ monthName(m) }}</option>
          </select>
        </label>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="data && !hasMonth" class="banner banner-warn">
      За {{ monthName(month) }} {{ year }} в стратегическом сценарии нет статей по площадкам — доли не заданы.
    </p>

    <SkeletonTable v-if="loading && !data" :rows="10" :cols="8" />

    <section v-if="data && hasMonth" class="card">
      <div class="table-scroll">
        <table class="data-table">
          <thead>
            <tr>
              <th>Площадка</th>
              <th v-for="b in BLOCKS" :key="b.key" class="col-num" :title="b.name">{{ b.short }}</th>
              <th class="col-num">Σ</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="seg in ['large', 'small']" :key="seg">
              <tr class="row-seg"><td :colspan="BLOCKS.length + 2">{{ seg === "large" ? "Крупные МП" : "Мелкие МП" }}</td></tr>
              <tr v-for="p in platformsOf(seg)" :key="p.code_cfo">
                <td>{{ p.name_cfo }} <span class="muted">· {{ p.country }}</span></td>
                <td v-for="b in BLOCKS" :key="b.key" class="col-num">{{ pct(share(p.code_cfo, b.key)) }}</td>
                <td class="col-num strong">{{ pct(total(p.code_cfo)) }}</td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import SkeletonTable from "~/components/SkeletonTable.vue";
import { retailErrText, useRetail, type StrategyMpShares } from "~/composables/useRetail";

definePageMeta({ middleware: ["scope-guard"] });

const BLOCKS = [
  { key: "cost_agent", short: "Агент.", name: "64 · Агентское (комиссионное) вознаграждение" },
  { key: "cost_freight", short: "Экспорт", name: "51 · Грузоперевозки экспорт" },
  { key: "cost_log_transport", short: "Развоз", name: "52 · Грузоперевозки развоз и перемещение" },
  { key: "cost_log_warehouse", short: "Аренда", name: "54 · Аренда помещений (стоянки)" },
  { key: "cost_ads", short: "Реклама", name: "13 · Реклама и маркетинг" },
  { key: "cost_ads_social", short: "Соцсети", name: "15 · Реклама-соц.сети" },
  { key: "cost_packaging", short: "Упак.", name: "45 · Расходы на упаковку" },
  { key: "cost_acquiring", short: "Эквайр.", name: "58 · Эквайринг" },
  { key: "cost_it", short: "ИТ", name: "48 · Расходы на IT" },
  { key: "cost_other", short: "Прочие", name: "66 · Прочие удержания и компенсации" }
];

const api = useRetail();
const now = new Date();
const year = ref(now.getFullYear());
// Тактика ведётся на следующий месяц — его и показываем первым.
const month = ref(now.getMonth() === 11 ? 12 : now.getMonth() + 2);
const years = [now.getFullYear() - 1, now.getFullYear(), now.getFullYear() + 1];
const data = ref<StrategyMpShares | null>(null);
const loading = ref(false);
const error = ref("");

const monthName = (m: number): string =>
  new Intl.DateTimeFormat("ru-RU", { month: "long" }).format(new Date(2000, m - 1, 1));
const monthShares = computed(() => data.value?.months.find((m) => m.month === month.value)?.shares || null);
const hasMonth = computed(() => !!monthShares.value && Object.keys(monthShares.value).length > 0);
const platformsOf = (seg: string) => (data.value?.platforms || []).filter((p) => p.segment === seg);
const share = (cfo: number, block: string): number | null => monthShares.value?.[String(cfo)]?.[block] ?? null;
const total = (cfo: number): number | null => {
  const sh = monthShares.value?.[String(cfo)];
  return sh ? Object.values(sh).reduce((a, v) => a + v, 0) : null;
};
const pct = (v: number | null): string => (v == null ? "—" : `${(v * 100).toFixed(2)} %`);

const load = async () => {
  loading.value = true;
  error.value = "";
  try {
    data.value = await api.strategyMpShares(year.value);
  } catch (e) {
    error.value = retailErrText(e, "Не удалось загрузить стратегию");
  } finally {
    loading.value = false;
  }
};
onMounted(load);
</script>

<style scoped>
.page-strategy { display: flex; flex-direction: column; gap: var(--sp-4); }
.form-field.inline { display: flex; align-items: center; gap: var(--sp-2); }
.table-scroll { overflow-x: auto; }
.row-seg td { font-weight: 600; color: var(--text-muted); background: var(--bg-surface-2); }
.muted { color: var(--text-muted); }
.col-num { font-family: var(--font-mono); font-variant-numeric: tabular-nums; text-align: right; white-space: nowrap; }
.strong { font-weight: 600; }
</style>
