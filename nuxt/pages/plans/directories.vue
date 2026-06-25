<template>
  <div class="page-dirs">
    <header class="page-header">
      <div>
        <h1 class="page-title">Справочники</h1>
        <p class="page-subtitle">НСИ модуля «Тактические планы»</p>
      </div>
    </header>

    <p v-if="error" class="error-banner">{{ error }}</p>
    <div class="dir-grid">
      <button
        v-for="d in dirs"
        :key="d.code"
        type="button"
        class="dir-card"
        :class="{ active: active === d.code }"
        @click="open(d.code)"
      >
        <span class="dir-code">{{ d.code }}</span>
        <span class="dir-meta">{{ d.source }} · {{ d.sync_status }} · {{ d.row_count }} строк</span>
      </button>
    </div>

    <section v-if="active" class="card">
      <h2 class="card-title">{{ active }}</h2>
      <table class="data-table report-table">
        <thead>
          <tr>
            <th v-for="k in columns" :key="k">{{ k }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in rows" :key="i">
            <td v-for="k in columns" :key="k" :class="{ 'col-num': isNum(row[k]) }">{{ row[k] }}</td>
          </tr>
        </tbody>
      </table>
    </section>
  </div>
</template>

<script setup lang="ts">
import { usePlans, type PlanDirectory } from "~/composables/usePlans";

definePageMeta({ middleware: "scope-guard" });

const { directories, directoryRows } = usePlans();

const dirs = ref<PlanDirectory[]>([]);
const active = ref("");
const rows = ref<Record<string, unknown>[]>([]);
const error = ref("");

const columns = computed(() => (rows.value.length ? Object.keys(rows.value[0]) : []));
const isNum = (v: unknown) => typeof v === "number";

const open = async (code: string) => {
  active.value = code;
  error.value = "";
  try {
    rows.value = await directoryRows(code);
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка загрузки справочника";
    rows.value = [];
  }
};

onMounted(async () => {
  try {
    dirs.value = await directories();
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка загрузки";
  }
});
</script>

<style scoped>
.dir-grid {
  display: flex;
  flex-wrap: wrap;
  gap: var(--sp-4);
  margin-bottom: var(--sp-6);
}
.dir-card {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: var(--sp-4) var(--sp-5);
  border: 1px solid var(--border, #e5e7eb);
  border-radius: var(--radius, 8px);
  background: var(--bg-card, #fff);
  cursor: pointer;
  text-align: left;
}
.dir-card.active {
  border-color: var(--accent, #4338ca);
}
.dir-code {
  font-family: var(--font-mono);
  font-weight: 600;
}
.dir-meta {
  font-size: var(--fs-sm, 12px);
  color: var(--fg-muted, #6b7280);
}
.error-banner {
  color: var(--neg, #b42318);
  padding: var(--sp-4) 0;
}
</style>
