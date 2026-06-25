<template>
  <div class="page-mp-form">
    <header class="page-header">
      <div>
        <h1 class="page-title">Маркетплейсы — {{ segment }}</h1>
        <p class="page-subtitle">TPL-MP, этап 1.1 · ввод тактики (VS3)</p>
      </div>
      <div class="page-actions">
        <span v-if="savedAt" class="saved-note">Сохранено в {{ savedAt }}</span>
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
    </div>

    <p v-if="error" class="error-banner">{{ error }}</p>
    <p v-else-if="loading">Загрузка…</p>
    <MpForm v-else-if="form" :form="form" />
  </div>
</template>

<script setup lang="ts">
import MpForm from "~/components/plans/MpForm.vue";
import { usePlanForm } from "~/composables/usePlanForm";

definePageMeta({ middleware: "scope-guard" });

const route = useRoute();
const segment = computed<"large" | "small">(() => (route.params.segment === "small" ? "small" : "large"));

const year = ref(2026);
const month = ref(5);

const { form, loading, saving, error, savedAt, load, save } = usePlanForm(segment, year, month);

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
</style>
