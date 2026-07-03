<template>
  <div class="page-route">
    <header class="page-header">
      <div>
        <h1 class="page-title">Маршрут процесса</h1>
        <p class="page-subtitle">Этапы: формы ввода, задания по ЦФО и основной согласующий — из назначений</p>
      </div>
      <NuxtLink to="/plans/admin/users" class="btn btn-ghost"><Icon name="lucide:users" /> Пользователи и права</NuxtLink>
    </header>

    <p v-if="!canEdit" class="banner banner-warn">Просмотр. Редактирование — у роли «Администратор ТП».</p>
    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="saved" class="banner banner-pos">Сохранено.</p>

    <div v-for="tr in tracks" :key="tr.key" class="track-block">
      <h2 class="track-title">{{ tr.label }}</h2>

      <div v-for="s in byTrack(tr.key)" :key="s.stage_code" class="stage card">
        <div class="st-head">
          <div class="st-id">
            <span class="st-code">{{ s.stage_code }}</span>
            <span class="st-name">{{ s.name }}</span>
          </div>
          <span class="st-due">{{ s.prev25 ? "25-е (М−1)" : s.due_rd ? s.due_rd + "-й р.д." : "—" }}</span>
        </div>

        <div class="st-cols">
          <!-- Колонка 1: согласующий этапа -->
          <div class="st-col">
            <div class="st-label"><Icon name="lucide:check-check" /> Основной согласующий этапа</div>
            <div v-if="approversFor(s.stage_code).length" class="ppl">
              <div v-for="a in approversFor(s.stage_code)" :key="a.id" class="person approver">
                <Icon name="lucide:user-check" class="p-ic" />
                <span class="p-name">{{ a.user_name }}</span>
                <span class="p-pos">{{ positionName(a.role) }}</span>
                <span v-if="a.legal_entity" class="badge p-b">{{ a.legal_entity }}</span>
              </div>
            </div>
            <p v-else class="st-empty">
              <span class="st-ref">эталон ТЗ: {{ s.responsible || "—" }}</span>
              <NuxtLink v-if="canEdit" to="/plans/admin/users" class="st-link">назначить →</NuxtLink>
            </p>
          </div>

          <!-- Колонка 2: формы ввода + задания по ЦФО -->
          <div class="st-col">
            <div class="st-label"><Icon name="lucide:clipboard-list" /> Задания на ввод (формы)</div>
            <div v-if="formsFor(s.stage_code).length" class="forms">
              <span v-for="f in formsFor(s.stage_code)" :key="f" class="form-chip">{{ f }}</span>
            </div>
            <p v-else class="st-noforms">этап без форм ввода (только согласование)</p>

            <div v-if="fillersFor(s.stage_code).length" class="ppl">
              <div v-for="a in fillersFor(s.stage_code)" :key="a.id" class="person">
                <Icon name="lucide:user" class="p-ic" />
                <span class="p-name">{{ a.user_name }}</span>
                <span v-if="a.country" class="badge p-b">{{ a.country }}</span>
                <span v-if="a.code_cfo?.length" class="p-cfo">ЦФО: {{ a.code_cfo.length }} ({{ a.code_cfo.slice(0, 6).join(", ") }}{{ a.code_cfo.length > 6 ? "…" : "" }})</span>
                <span v-else class="p-cfo p-muted">весь срез</span>
              </div>
            </div>
            <p v-else-if="formsFor(s.stage_code).length" class="st-empty">
              нет ответственных
              <NuxtLink v-if="canEdit" to="/plans/admin/users" class="st-link">назначить →</NuxtLink>
            </p>
          </div>
        </div>

        <!-- Эталон ТЗ (справочно, редактируемый) -->
        <details class="st-ref-block">
          <summary>Эталон ТЗ (справочно)</summary>
          <div class="rr-edit">
            <input v-model="s.responsible" class="input" :disabled="!canEdit" placeholder="ответственные / согласующие по схеме" />
            <button v-if="canEdit" class="btn btn-sm btn-ghost" @click="save(s)"><Icon name="lucide:save" /></button>
          </div>
        </details>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { usePlans, type StageRoute } from "~/composables/usePlans";
import { usePlansRights, type ScopeAssignment, type Position } from "~/composables/usePlansRights";

definePageMeta({ middleware: "scope-guard" });

const { route: loadRoute, setRoute } = usePlans();
const rights = usePlansRights();
const { hasRole, isAdmin } = useScope();
const canEdit = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));

const list = ref<StageRoute[]>([]);
const assignments = ref<ScopeAssignment[]>([]);
const positions = ref<Position[]>([]);
const error = ref("");
const saved = ref(false);

const tracks = [
  { key: "sales", label: "Поток продаж" },
  { key: "production", label: "Поток производства" },
  { key: "final", label: "Финал — ЮЛ" }
];

// Формы ввода по этапам (ТЗ Приложение G / модель данных).
const STAGE_FORMS: Record<string, string[]> = {
  "1.1": ["TPL-TO-RETAIL · Розница", "TPL-MP · Маркетплейсы", "TPL-WHOLESALE · Опт", "TPL-IM · Интернет-магазин"],
  "1.5": ["TPL-CFO-EXP · Затраты ЦП"],
  "1.6": ["Свод ЮЛ × канал"],
  "2.1": ["TPL-PROD-MINUTES · Минуты и объёмы"],
  "2.3": ["TPL-CFO-EXP · Затраты ЦЗ"],
  "2.4": ["Свод по ЮЛ"]
};

const byTrack = (k: string) => list.value.filter((s) => s.track === k);
const positionName = (code: string) => positions.value.find((p) => p.code === code)?.name ?? code;
const positionKind = (code: string) => positions.value.find((p) => p.code === code)?.kind ?? "filler";
const assignedFor = (stage: string) => assignments.value.filter((a) => a.stage_code === stage);
const approversFor = (stage: string) => assignedFor(stage).filter((a) => ["approver", "coordinator"].includes(positionKind(a.role)));
const fillersFor = (stage: string) => assignedFor(stage).filter((a) => positionKind(a.role) === "filler");
const formsFor = (stage: string) => STAGE_FORMS[stage] ?? [];

const save = async (s: StageRoute) => {
  error.value = "";
  saved.value = false;
  try {
    await setRoute(s.stage_code, s.responsible);
    saved.value = true;
    setTimeout(() => (saved.value = false), 2000);
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка сохранения";
  }
};

onMounted(async () => {
  try {
    list.value = await loadRoute();
    if (canEdit.value) {
      [assignments.value, positions.value] = await Promise.all([rights.assignments(), rights.positions()]);
    }
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : "Ошибка загрузки";
  }
});
</script>

<style scoped>
.banner { padding: var(--sp-4) var(--sp-5); border-radius: var(--rd-4, 6px); margin-bottom: var(--sp-5); font-size: var(--fs-sm); }
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
.banner-warn { background: var(--warn-soft); color: var(--warn); }
.banner-pos { background: var(--pos-soft); color: var(--pos-strong); }
.track-block { margin-bottom: var(--sp-6); }
.track-title { font-size: var(--fs-sm); color: var(--text-secondary); margin-bottom: var(--sp-3); }

.stage { padding: var(--sp-4) var(--sp-5); margin-bottom: var(--sp-3); }
.st-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: var(--sp-3); }
.st-id { display: flex; align-items: baseline; gap: var(--sp-3); }
.st-code { font-family: var(--font-mono); font-weight: var(--fw-bold); font-size: var(--fs-md); color: var(--accent); }
.st-name { font-size: var(--fs-sm); color: var(--text-strong); }
.st-due { font-size: var(--fs-2xs); color: var(--text-muted); }

.st-cols { display: grid; grid-template-columns: 1fr 1.4fr; gap: var(--sp-5); align-items: start; }
.st-col { display: flex; flex-direction: column; gap: var(--sp-2); }
.st-label { display: flex; align-items: center; gap: var(--sp-2); font-size: var(--fs-xs); color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.03em; }

.forms { display: flex; flex-wrap: wrap; gap: var(--sp-2); }
.form-chip { font-size: var(--fs-2xs); padding: 2px 8px; border: 1px dashed var(--border); border-radius: var(--rd-3); color: var(--text-secondary); font-family: var(--font-mono); }
.st-noforms { font-size: var(--fs-xs); color: var(--text-muted); margin: 0; }

.ppl { display: flex; flex-direction: column; gap: var(--sp-2); margin-top: var(--sp-2); }
.person { display: flex; align-items: center; gap: var(--sp-3); font-size: var(--fs-sm); padding: var(--sp-2) var(--sp-3); background: var(--bg-tonal); border-radius: var(--rd-3); flex-wrap: wrap; }
.person.approver { background: var(--pos-soft); }
.p-ic { color: var(--text-secondary); flex-shrink: 0; }
.p-name { font-weight: var(--fw-semibold); }
.p-pos { color: var(--text-secondary); font-size: var(--fs-2xs); }
.p-b { font-size: var(--fs-2xs); }
.p-cfo { margin-left: auto; font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-secondary); }
.p-muted { color: var(--text-muted); }

.st-empty { font-size: var(--fs-sm); color: var(--text-muted); margin: var(--sp-2) 0 0; }
.st-ref { font-style: italic; }
.st-link { color: var(--accent); margin-left: var(--sp-2); }

.st-ref-block { margin-top: var(--sp-3); border-top: 1px solid var(--border); padding-top: var(--sp-2); }
.st-ref-block summary { font-size: var(--fs-2xs); color: var(--text-muted); cursor: pointer; }
.rr-edit { display: flex; gap: var(--sp-3); margin-top: var(--sp-2); }
.rr-edit .input { flex: 1; }
</style>
