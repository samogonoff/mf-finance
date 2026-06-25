<template>
  <div class="page-mp-form">
    <header class="page-header">
      <div>
        <h1 class="page-title">Маркетплейсы — {{ segment }}</h1>
        <p class="page-subtitle">TPL-MP, этап 1.1 · ввод тактики (VS3)</p>
      </div>
      <div class="page-actions">
        <span v-if="savedAt" class="saved-note">Сохранено в {{ savedAt }}</span>
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

    <p v-if="error" class="error-banner">{{ error }}</p>
    <p v-else-if="loading">Загрузка…</p>
    <MpForm v-else-if="form" :form="form" />
  </div>
</template>

<script setup lang="ts">
import MpForm from "~/components/plans/MpForm.vue";
import { usePlanForm } from "~/composables/usePlanForm";
import { usePlans } from "~/composables/usePlans";

definePageMeta({ middleware: "scope-guard" });

const route = useRoute();
const segment = computed<"large" | "small">(() => (route.params.segment === "small" ? "small" : "large"));

const year = ref(2026);
const month = ref(5);
const currency = ref<"RUB" | "BYN" | "USD">("RUB");

const { form, loading, saving, error, savedAt, reason, load, save } = usePlanForm(segment, year, month, currency);
const { mpExport, mpImport } = usePlans();

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
.error-banner {
  color: var(--neg, #b42318);
  padding: var(--sp-4);
}
.hidden-input {
  display: none;
}
</style>
