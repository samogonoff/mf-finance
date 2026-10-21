<!--
  Комплект форм этапа 1.5 «Расходы центров прибыли» (ТЗ §1, §3, §4).

  Зачем отдельный экран, а не переход сразу в форму: задание этапа 1.5 ОДНО
  («Расходы ЦП», 145 ЦФО), а форм четыре — розница РБ и три страны
  маркетплейсов, у каждой свой отбор строк из источника и своя карточка
  процесса. По ЦФО задания страну не выбрать, поэтому кнопка «Форма» из списка
  заданий ведёт сюда, а отсюда — в нужную форму.
-->
<template>
  <div class="page-exp-hub">
    <header class="page-header">
      <div>
        <h1 class="page-title">Расходы ЦП · {{ periodLabel }}</h1>
        <p class="page-subtitle">
          Этап 1.5 · формы заполняются на плановый год целиком · {{ cards.length }} формы комплекта
        </p>
      </div>
      <div class="page-actions">
        <NuxtLink :to="`/plans/${plId}`" class="btn btn-ghost"><Icon name="lucide:arrow-left" /> К периоду</NuxtLink>
        <NuxtLink :to="`/plans/process/${plId}`" class="btn btn-ghost"><Icon name="lucide:list-checks" /> Процесс</NuxtLink>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p class="banner banner-info">
      Реализованы состав строк и ввод по месяцам. Реестры ФОТ / командировок / логистики / ИТ,
      расчёты ФОТ и аренды, свод с контрольными сверками и публикация — следующие фазы;
      публикация до решения по общим затратам МП намеренно выключена в маршруте.
    </p>

    <p v-if="loading" class="hub-loading"><Icon name="lucide:loader-circle" class="spin" /> Загружаю карточки периода…</p>
    <p v-else-if="!cards.length" class="hub-empty">
      Карточки форм на период не созданы. Создайте их на странице периода — кнопка «Карточка PL».
    </p>

    <div v-else class="hub-grid">
      <article v-for="c in cards" :key="c.id" class="card hub-card">
        <div class="hc-top">
          <span class="hc-title">{{ scopeTitle(c) }}</span>
          <span class="badge" :class="statusTone(c.status)">{{ c.status_label || c.status }}</span>
        </div>
        <div class="hc-meta">
          <span class="hc-mono">{{ c.scope_key }}</span>
          <span>· {{ c.country || "—" }}</span>
          <span>· ввод в {{ c.currency || "—" }}</span>
          <span>· версия {{ c.current_version }}</span>
          <span v-if="c.locked" class="hc-lock"><Icon name="lucide:lock" /> закрыт на запись</span>
        </div>
        <div class="hc-actions">
          <NuxtLink :to="`/plans/exp-form/${c.id}`" class="btn btn-sm btn-primary">
            <Icon name="lucide:pencil" /> Открыть форму
          </NuxtLink>
        </div>
      </article>
    </div>
  </div>
</template>

<script setup lang="ts">
import { apiError } from "~/utils/apiError";
import { useExpForm, type ExpCard } from "~/composables/useExpForm";

const route = useRoute();
const plId = Number(route.params.plId);
const api = useExpForm();

const cards = ref<ExpCard[]>([]);
const loading = ref(true);
const error = ref("");

const periodLabel = computed(() => {
  const c = cards.value[0];
  return c ? `${c.year}-${String(c.month).padStart(2, "0")}` : "период";
});

/** Заголовок карточки: своё название формы понятнее, чем «Расходы ЦП · Розница РБ». */
const scopeTitle = (c: ExpCard) => {
  const suffix = c.title.includes("·") ? c.title.split("·").slice(1).join("·").trim() : c.title;
  return c.scope_key.startsWith("MP") ? `Маркетплейсы ${c.country}` : suffix || c.title;
};

const statusTone = (s: string) =>
  s === "approved" || s === "published" ? "badge-pos" : s === "returned" ? "badge-neg" : s === "on_approval" ? "badge-warn" : "badge-muted";

const load = async () => {
  loading.value = true;
  error.value = "";
  try {
    const all = await api.cards(plId);
    cards.value = all.filter((c) => c.form_code === "TPL-CFO-EXP");
  } catch (e) {
    error.value = apiError(e, "Не удалось загрузить карточки периода");
  } finally {
    loading.value = false;
  }
};
onMounted(load);
</script>

<style scoped>
.page-exp-hub { display: flex; flex-direction: column; gap: var(--sp-5); }
.hub-loading, .hub-empty { font-size: var(--fs-sm); color: var(--text-secondary); margin: 0; display: flex; align-items: center; gap: 6px; }
.hub-grid {
  display: grid;
  /* Карточка комплекта не должна растягиваться во всю ширину монитора и не
     должна ужиматься в непригодную полоску — отсюда и минимум, и максимум. */
  grid-template-columns: repeat(auto-fill, minmax(280px, 360px));
  gap: var(--sp-4);
}
.hub-card { display: flex; flex-direction: column; gap: var(--sp-3); padding: var(--sp-5); min-width: 0; }
.hc-top { display: flex; align-items: center; justify-content: space-between; gap: var(--sp-3); flex-wrap: wrap; }
.hc-title { font-size: var(--fs-md); font-weight: var(--fw-semibold); color: var(--text-strong); overflow-wrap: anywhere; }
.hc-meta { display: flex; flex-wrap: wrap; gap: var(--sp-2); font-size: var(--fs-2xs); color: var(--text-muted); min-width: 0; }
.hc-mono { font-family: var(--font-mono); }
.hc-lock { color: var(--neg); display: inline-flex; align-items: center; gap: 3px; }
.hc-actions { display: flex; flex-wrap: wrap; gap: var(--sp-3); margin-top: auto; }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
