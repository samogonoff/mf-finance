<template>
  <div class="page-pl-card">
    <header class="page-header">
      <div>
        <h1 class="page-title">Карточка PL · {{ year }}-{{ String(month).padStart(2, "0") }}</h1>
        <p class="page-subtitle">Согласование по схеме (этапы 1.1–4) · сроки по календарю р.д.</p>
      </div>
      <NuxtLink :to="`/plans/mp/large?year=${year}&month=${month}`" class="btn btn-ghost">
        <Icon name="lucide:store" /> Форма МП
      </NuxtLink>
    </header>

    <p v-if="error" class="error-banner">{{ error }}</p>

    <section class="svod">
      <h2 class="track-title">Свод по ЮЛ × каналам (TPL-08, товарооборот)</h2>
      <table class="data-table report-table">
        <thead>
          <tr><th>ЮЛ</th><th>Канал</th><th class="col-num">Товарооборот</th><th>Вал.</th></tr>
        </thead>
        <tbody>
          <tr v-if="!svod.length"><td colspan="4" class="empty">Нет данных тактики за период.</td></tr>
          <tr v-for="(s, i) in svod" :key="i">
            <td>{{ s.legal_entity }}</td>
            <td>{{ s.channel }}</td>
            <td class="col-num">{{ money(s.amount, { currency: "" }) }}</td>
            <td>{{ s.currency }}</td>
          </tr>
        </tbody>
      </table>
      <p class="prov-note">Маппинг площадка→ЮЛ провизорный (уточнить у аналитика).</p>
    </section>

    <div v-for="track in tracks" :key="track.key" class="track">
      <h2 class="track-title">{{ track.label }}</h2>
      <div class="stage-list">
        <div
          v-for="st in byTrack(track.key)"
          :key="st.stage_code"
          class="stage"
          :class="`status-${st.status}`"
        >
          <div class="stage-main">
            <span class="stage-code">{{ st.stage_code }}</span>
            <span class="stage-name">{{ st.name }}</span>
          </div>
          <div class="stage-meta">
            <span class="stage-due">срок: {{ st.due_at || "—" }}</span>
            <span class="stage-status">{{ st.status }}</span>
          </div>
          <div class="stage-actions">
            <button class="btn btn-ghost btn-xs" @click="act(st.stage_code, 'submit')">Отправить</button>
            <button class="btn btn-ghost btn-xs" @click="act(st.stage_code, 'approve')">Согласовать</button>
            <button class="btn btn-ghost btn-xs" @click="returnTo(st.stage_code)">Вернуть</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { money } from "~/utils/format";
import { usePlans, type StageState, type SvodRow } from "~/composables/usePlans";

definePageMeta({ middleware: "scope-guard" });

const route = useRoute();
const id = Number(route.params.id);
const year = ref(Number(route.query.year) || 2026);
const month = ref(Number(route.query.month) || 6);

const { stages, stageAction, mpSvod } = usePlans();
const list = ref<StageState[]>([]);
const svod = ref<SvodRow[]>([]);
const error = ref("");

const tracks = [
  { key: "sales", label: "Поток продаж" },
  { key: "production", label: "Поток производства" },
  { key: "final", label: "Финал (ЮЛ)" }
];
const byTrack = (k: string) => list.value.filter((s) => s.track === k);

const load = async () => {
  error.value = "";
  try {
    list.value = await stages(id, year.value, month.value);
    svod.value = await mpSvod(year.value, month.value);
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка загрузки";
  }
};

const act = async (code: string, action: string, target?: string) => {
  error.value = "";
  try {
    list.value = await stageAction(id, code, { action, target, year: year.value, month: month.value });
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка перехода";
  }
};

const returnTo = (code: string) => {
  const target = window.prompt("Вернуть на этап (код, напр. 1.1):", "");
  if (target) act(code, "return", target);
};

onMounted(load);
</script>

<style scoped>
.track {
  margin-bottom: var(--sp-6);
}
.track-title {
  font-size: var(--fs-base, 13px);
  color: var(--fg-muted, #6b7280);
  margin-bottom: var(--sp-3, 6px);
}
.stage {
  display: flex;
  align-items: center;
  gap: var(--sp-5);
  padding: var(--sp-4) var(--sp-5);
  border: 1px solid var(--border, #e5e7eb);
  border-left: 3px solid var(--border, #e5e7eb);
  border-radius: var(--radius, 8px);
  margin-bottom: var(--sp-3, 6px);
  flex-wrap: wrap;
}
.status-completed {
  border-left-color: var(--pos, #0a7f3f);
}
.status-returned {
  border-left-color: var(--neg, #b42318);
}
.status-in_progress {
  border-left-color: var(--accent, #4338ca);
}
.stage-main {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  flex: 1;
  min-width: 220px;
}
.stage-code {
  font-family: var(--font-mono);
  font-weight: 600;
  min-width: 32px;
}
.stage-meta {
  display: flex;
  gap: var(--sp-4);
  font-size: var(--fs-sm, 12px);
  color: var(--fg-muted, #6b7280);
}
.stage-actions {
  display: flex;
  gap: var(--sp-3, 6px);
}
.btn-xs {
  font-size: var(--fs-sm, 12px);
  padding: 2px 8px;
}
.error-banner {
  color: var(--neg, #b42318);
  padding: var(--sp-4) 0;
}
</style>
