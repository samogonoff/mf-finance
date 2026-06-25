<template>
  <div class="page-plans-mp">
    <header class="page-header">
      <div>
        <h1 class="page-title">Маркетплейсы — факт</h1>
        <p class="page-subtitle">TPL-MP, этап 1.1 · read-only факт (VS2). Ввод тактики — VS3.</p>
      </div>
    </header>

    <div class="filters-bar">
      <div class="filter">
        <label class="filter-label">Год</label>
        <input v-model.number="year" type="number" class="select" min="2024" max="2030" />
      </div>
      <div class="filter">
        <label class="filter-label">Месяц</label>
        <input v-model.number="month" type="number" class="select" min="1" max="12" />
      </div>
      <div class="filter">
        <label class="filter-label">Сегмент</label>
        <div class="chip-row">
          <button
            v-for="s in (['large', 'small'] as const)"
            :key="s"
            type="button"
            class="chip"
            :class="{ active: segment === s }"
            @click="segment = s"
          >
            {{ s }}
          </button>
        </div>
      </div>
      <button class="btn btn-primary" :disabled="loading" @click="load">
        <Icon name="lucide:refresh-cw" /> Обновить
      </button>
    </div>

    <p v-if="error" class="error-banner">{{ error }}</p>
    <MpFactTable v-else :rows="rows" :currency="rows[0]?.currency || 'RUB'" />
  </div>
</template>

<script setup lang="ts">
import MpFactTable from "~/components/plans/MpFactTable.vue";
import { usePlans, type PlanFactRow } from "~/composables/usePlans";

definePageMeta({ middleware: "scope-guard" });

const { mpFact } = usePlans();

const year = ref(2026);
const month = ref(5);
const segment = ref<"large" | "small">("large");
const rows = ref<PlanFactRow[]>([]);
const loading = ref(false);
const error = ref("");

const load = async () => {
  loading.value = true;
  error.value = "";
  try {
    rows.value = await mpFact({ year: year.value, month: month.value, segment: segment.value });
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка загрузки факта";
    rows.value = [];
  } finally {
    loading.value = false;
  }
};

onMounted(load);
</script>

<style scoped>
.filters-bar {
  display: flex;
  align-items: flex-end;
  gap: var(--sp-5);
  margin-bottom: var(--sp-6);
  flex-wrap: wrap;
}
.error-banner {
  color: var(--neg, #b42318);
  padding: var(--sp-4);
}
</style>
