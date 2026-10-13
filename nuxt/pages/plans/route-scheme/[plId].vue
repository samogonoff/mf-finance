<!--
  Схема согласования «как её увидит согласующий» (ТЗ доработок кабинета, п. 4.3).

  Собирается не из документов, а из системы: включённые шаги маршрута формы
  (выключенных здесь нет — по ним карточка не пойдёт) и исполнители, которых
  определит тот же механизм, что и при реальном переходе. Шаг согласования без
  исполнителя подсвечен как пробел (п. 4.2): на нём карточка остановится.
-->
<template>
  <div class="page-scheme">
    <header class="page-header">
      <div>
        <h1 class="page-title">Схема согласования</h1>
        <p class="page-subtitle">Розница и маркетплейсы · по действующим настройкам маршрута и назначениям</p>
      </div>
      <div class="page-actions">
        <NuxtLink :to="`/plans/${plId}`" class="btn btn-sm btn-ghost">
          <Icon name="lucide:arrow-left" /> К периоду
        </NuxtLink>
        <button class="btn btn-sm btn-ghost" :disabled="loading" @click="load">
          <Icon name="lucide:refresh-cw" :class="{ spin: loading }" /> Обновить
        </button>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="totalGaps" class="banner banner-warn">
      Не назначены исполнители на {{ totalGaps }} шаг(ах) — на них карточки остановятся. Назначьте их в правах-срезах планов.
    </p>
    <p v-else-if="cards.length" class="banner banner-pos">Исполнители назначены на всех шагах согласования.</p>

    <SkeletonTable v-if="loading && !cards.length" :rows="6" :cols="5" />

    <section v-for="c in cards" :key="c.card_id" class="card scheme-card">
      <div class="card-header">
        <span class="card-title">{{ c.title || `${c.form_code} · ${c.scope_key}` }}</span>
        <span class="muted">{{ c.status }}</span>
        <span v-if="c.gaps" class="badge badge-warn">пробелов: {{ c.gaps }}</span>
      </div>
      <p v-if="c.error" class="banner banner-warn">{{ c.error }}</p>
      <ol class="steps">
        <li v-for="s in c.steps" :key="s.step_code" class="step" :class="{ gap: s.gap, current: s.current }">
          <span class="step-code">{{ s.step_code }}</span>
          <span class="step-name">{{ s.step_name }}</span>
          <span class="step-kind">{{ KIND[s.kind] || s.kind }}</span>
          <span class="step-owner">
            <template v-if="s.owner_name || s.owner_user_id">{{ s.owner_name || `пользователь #${s.owner_user_id}` }}</template>
            <template v-else-if="s.gap">не назначен</template>
            <template v-else>исполнитель задания</template>
          </span>
          <span v-if="s.responsible" class="step-resp" :title="s.responsible">{{ s.responsible }}</span>
          <span v-if="s.current" class="badge badge-info">сейчас здесь</span>
        </li>
      </ol>
    </section>
  </div>
</template>

<script setup lang="ts">
import SkeletonTable from "~/components/SkeletonTable.vue";
import { retailErrText, useRetail, type RouteSchemeCard } from "~/composables/useRetail";

definePageMeta({ middleware: ["scope-guard"] });

const KIND: Record<string, string> = { fill: "заполнение", approve: "согласование", final: "утверждение" };

const route = useRoute();
const plId = Number(route.params.plId);
const api = useRetail();
const cards = ref<RouteSchemeCard[]>([]);
const loading = ref(false);
const error = ref("");
const totalGaps = computed(() => cards.value.reduce((a, c) => a + c.gaps, 0));

const load = async () => {
  loading.value = true;
  error.value = "";
  try {
    cards.value = (await api.routeScheme(plId)).cards;
  } catch (e) {
    error.value = retailErrText(e, "Не удалось загрузить схему согласования");
  } finally {
    loading.value = false;
  }
};
onMounted(load);
</script>

<style scoped>
.page-scheme { display: flex; flex-direction: column; gap: var(--sp-4); }
.scheme-card .card-header { display: flex; align-items: center; gap: var(--sp-3); }
.muted { color: var(--text-muted); font-size: var(--fs-sm); }
.steps { list-style: none; margin: 0; padding: var(--sp-3) var(--sp-4); display: flex; flex-direction: column; gap: var(--sp-2); }
.step {
  display: grid;
  grid-template-columns: 56px minmax(160px, 1.2fr) 120px minmax(160px, 1fr) minmax(0, 1.5fr) auto;
  gap: var(--sp-3);
  align-items: center;
  padding: var(--sp-2) var(--sp-3);
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  font-size: var(--fs-sm);
}
.step.current { border-color: var(--accent); }
.step.gap { border-color: var(--warn); }
.step.gap .step-owner { color: var(--warn); font-weight: 600; }
.step-code { font-family: var(--font-mono); color: var(--text-muted); }
.step-kind { color: var(--text-muted); }
.step-resp { color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 720px) {
  .step { grid-template-columns: 48px 1fr; }
  .step-kind, .step-resp { grid-column: 2; }
}
</style>
