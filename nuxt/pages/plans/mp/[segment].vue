<template>
  <div class="page-mp-form">
    <header class="page-header">
      <div>
        <h1 class="page-title">Маркетплейсы — {{ segment }}</h1>
        <p class="page-subtitle">Бюджет продаж, этап 1.1 · ввод тактики по площадкам</p>
      </div>
      <div class="page-actions">
        <span v-if="savedAt" class="saved-note">Сохранено в {{ savedAt }}</span>
        <button class="btn btn-ghost" @click="cp.open = true">
          <Icon name="lucide:copy" /> Копировать период
        </button>
        <button class="btn btn-ghost" :disabled="!form" @click="exportXlsx">
          <Icon name="lucide:download" /> Excel
        </button>
        <button class="btn btn-ghost" @click="importInput?.click()">
          <Icon name="lucide:upload" /> Импорт
        </button>
        <input ref="importInput" type="file" accept=".xlsx" class="hidden-input" @change="onImport" />
        <button class="btn btn-primary" :disabled="saving || !form" @click="save">
          <Icon name="lucide:save" /> {{ saving ? "Сохранение…" : "Сохранить" }}
        </button>
      </div>
    </header>

    <div class="filters-bar">
      <div class="filter">
        <label class="filter-label">Год</label>
        <input v-model.number="year" type="number" class="select" min="2024" max="2030" @change="load" />
      </div>
      <div class="filter">
        <label class="filter-label">Месяц</label>
        <input v-model.number="month" type="number" class="select" min="1" max="12" @change="load" />
      </div>
      <div class="filter">
        <label class="filter-label">Валюта</label>
        <div class="chip-row">
          <button
            v-for="c in (['RUB', 'BYN', 'USD'] as const)"
            :key="c"
            type="button"
            class="chip"
            :class="{ active: currency === c }"
            @click="setCurrency(c)"
          >
            {{ c }}
          </button>
        </div>
      </div>
      <div class="filter filter-grow">
        <label class="filter-label">Причина корректировки (обязательна при правке)</label>
        <input v-model="reason" type="text" class="select" placeholder="напр. корректировка на акцию" />
      </div>
    </div>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-else-if="loading" class="loading">Загрузка…</p>
    <template v-else-if="form">
      <MpForm :form="form" />
      <div class="compute-actions">
        <button class="btn btn-ghost" @click="runCompute">
          <Icon name="lucide:calculator" /> Рассчитать каскад
        </button>
        <button class="btn btn-ghost" @click="ov.open = true">
          <Icon name="lucide:function-square" /> Формула каскада…
        </button>
      </div>
      <MpComputePreview :rows="computed" />
    </template>

    <!-- Копирование тактики из прошлого периода (TPL-09) -->
    <PlansModal
      :open="cp.open"
      title="Скопировать план из периода"
      :subtitle="`Тактика будет скопирована в текущий период ${year}-${String(month).padStart(2,'0')} как стартовая точка`"
      @close="cp.open = false"
    >
      <div class="cp-period">
        <label class="field">
          <span class="field-label">Год-источник</span>
          <input v-model.number="cp.year" type="number" class="select" min="2024" max="2030" />
        </label>
        <label class="field">
          <span class="field-label">Месяц-источник</span>
          <input v-model.number="cp.month" type="number" class="select" min="1" max="12" />
        </label>
      </div>
      <p class="cp-hint">Скопируются ячейки тактики вашего ABAC-среза; существующие значения целевого периода перезапишутся.</p>
      <template #footer>
        <button class="btn btn-ghost" @click="cp.open = false">Отмена</button>
        <button class="btn btn-primary" @click="doCopy">
          <Icon name="lucide:copy" /> Скопировать
        </button>
      </template>
    </PlansModal>

    <!-- Переопределение формулы каскада (D11) -->
    <PlansModal
      :open="ov.open"
      title="Переопределить формулу каскада"
      subtitle="Действует на этот срез; требует причину"
      width="520px"
      @close="ov.open = false"
    >
      <label class="field">
        <span class="field-label">Показатель</span>
        <select v-model="ov.code" class="select">
          <option value="sales_net">sales_net — продажи без НДС</option>
          <option value="gross_margin">gross_margin — маржа (gross)</option>
          <option value="markup_pct">markup_pct — наценка, %</option>
        </select>
      </label>
      <label class="field">
        <span class="field-label">Формула (переменные: sales, cost, vat)</span>
        <input v-model="ov.expr" class="select mono" placeholder="напр. sales - 2 * cost" />
      </label>
      <label class="field">
        <span class="field-label">Причина</span>
        <input v-model="ov.reason" class="select" placeholder="напр. учёт пошива в себестоимости" />
      </label>
      <template #footer>
        <button class="btn btn-ghost" @click="ov.open = false">Отмена</button>
        <button class="btn btn-primary" :disabled="!ov.expr || !ov.reason" @click="saveOverride">
          <Icon name="lucide:check" /> Применить
        </button>
      </template>
    </PlansModal>
  </div>
</template>

<script setup lang="ts">
import MpForm from "~/components/plans/MpForm.vue";
import MpComputePreview from "~/components/plans/MpComputePreview.vue";
import PlansModal from "~/components/plans/PlansModal.vue";
import { usePlanForm } from "~/composables/usePlanForm";
import { usePlans, type ComputedRow } from "~/composables/usePlans";

definePageMeta({ middleware: "scope-guard" });

const route = useRoute();
const segment = computed<"large" | "small">(() => (route.params.segment === "small" ? "small" : "large"));

const year = ref(Number(route.query.year) || 2026);
const month = ref(Number(route.query.month) || 5);
const currency = ref<"RUB" | "BYN" | "USD">("RUB");

const { form, loading, saving, error, savedAt, reason, load, save } = usePlanForm(segment, year, month, currency);
const { mpExport, mpImport, mpCompute, saveFormula, copyMp } = usePlans();

const cp = reactive({ open: false, year: 2026, month: 4 });
const doCopy = async () => {
  error.value = "";
  try {
    const res = await copyMp({ from_year: cp.year, from_month: cp.month, to_year: year.value, to_month: month.value });
    cp.open = false;
    await load();
    if (res.copied === 0) error.value = "Из выбранного периода нечего копировать (нет тактики в вашем срезе).";
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка копирования";
  }
};

const computed = ref<ComputedRow[]>([]);
const runCompute = async () => {
  try {
    computed.value = await mpCompute({ year: year.value, month: month.value, segment: segment.value, currency: currency.value });
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка расчёта";
  }
};

const ov = reactive({ open: false, code: "gross_margin", expr: "", reason: "" });
const saveOverride = async () => {
  error.value = "";
  try {
    await saveFormula({
      year: year.value,
      month: month.value,
      code: ov.code,
      formula_expr: ov.expr,
      reason: ov.reason
    });
    ov.expr = "";
    ov.reason = "";
    ov.open = false;
    await runCompute();
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка override формулы";
  }
};

const setCurrency = (c: "RUB" | "BYN" | "USD") => {
  currency.value = c;
  load();
};

const importInput = ref<HTMLInputElement | null>(null);

const exportXlsx = async () => {
  try {
    const blob = await mpExport({ year: year.value, month: month.value, segment: segment.value, currency: currency.value });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `TPL-MP_${segment.value}_${year.value}-${String(month.value).padStart(2, "0")}.xlsx`;
    a.click();
    URL.revokeObjectURL(url);
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка экспорта";
  }
};

const onImport = async (ev: Event) => {
  const input = ev.target as HTMLInputElement;
  const file = input.files?.[0];
  if (!file) return;
  error.value = "";
  try {
    await mpImport({ year: year.value, month: month.value, segment: segment.value }, file);
    await load();
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка импорта";
  } finally {
    input.value = "";
  }
};

watch(segment, load);
onMounted(load);
</script>

<style scoped>
.filters-bar {
  display: flex;
  align-items: flex-end;
  gap: var(--sp-5);
  margin-bottom: var(--sp-6);
}
.saved-note {
  color: var(--pos, #0a7f3f);
  font-size: var(--fs-sm, 12px);
  margin-right: var(--sp-4);
}
.banner {
  padding: var(--sp-4) var(--sp-5);
  border-radius: var(--rd-4, 6px);
  font-size: var(--fs-sm);
  margin-bottom: var(--sp-5);
}
.banner-neg {
  background: var(--neg-soft);
  color: var(--neg-strong);
}
.loading {
  color: var(--text-muted);
  padding: var(--sp-5) 0;
}
.hidden-input {
  display: none;
}
.compute-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--sp-4);
  margin: var(--sp-5) 0;
}
.field {
  display: block;
  margin-bottom: var(--sp-5);
}
.field-label {
  display: block;
  font-size: var(--fs-sm);
  color: var(--text-secondary);
  margin-bottom: var(--sp-3);
}
.field .select {
  width: 100%;
}
.mono {
  font-family: var(--font-mono);
}
.cp-period {
  display: flex;
  gap: var(--sp-5);
}
.cp-hint {
  font-size: var(--fs-sm);
  color: var(--text-muted);
}
</style>
