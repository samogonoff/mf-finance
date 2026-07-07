<!--
  Конструктор шаблонов заданий. Шаблон = этап × форма × ЦФО-фильтр × роль × разрез.
  По шаблонам генерятся задания на период (исполнитель авто = ТОП ЦФО / Директор ЮЛ).
-->
<template>
  <div class="page-tt">
    <header class="page-header">
      <div>
        <h1 class="page-title">Конструктор заданий</h1>
        <p class="page-subtitle">Шаблоны заданий по этапам — из них генерятся задания периода</p>
      </div>
      <div class="page-actions">
        <NuxtLink to="/plans" class="btn btn-ghost"><Icon name="lucide:arrow-left" /> К модулю</NuxtLink>
        <button v-if="canEdit" class="btn btn-sm btn-primary" @click="openEdit()"><Icon name="lucide:plus" /> Шаблон</button>
      </div>
    </header>

    <p v-if="!canEdit" class="banner banner-warn">Просмотр. Редактирование — у роли «Администратор ТП».</p>
    <p v-if="error" class="banner banner-neg">{{ error }}</p>

    <div v-for="stage in stages" :key="stage" class="stage-group">
      <h2 class="sg-title">Этап {{ stage }}</h2>
      <div class="card tt-table">
        <table class="data-table">
          <thead><tr><th>Форма</th><th>Название</th><th>Фильтр ЦФО</th><th>Роль</th><th>Разрез</th><th class="th-act"></th></tr></thead>
          <tbody>
            <tr v-for="t in byStage(stage)" :key="t.id">
              <td><span class="cell-code">{{ t.form_code }}</span></td>
              <td>{{ t.title }}</td>
              <td class="td-filter">{{ filterLabel(t.cfo_filter) }}</td>
              <td><span class="badge" :class="t.task_role === 'approve' ? 'badge-accent' : 'badge-info'">{{ t.task_role === "approve" ? "согл." : "ввод" }}</span></td>
              <td>{{ groupLabel(t.group_by) }}</td>
              <td class="th-act">
                <button v-if="canEdit" class="btn btn-icon btn-sm" @click="openEdit(t)"><Icon name="lucide:pencil" /></button>
                <button v-if="canEdit" class="btn btn-icon btn-sm" @click="remove(t)"><Icon name="lucide:trash-2" /></button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div v-if="modal" class="modal-overlay" @click.self="modal = false">
      <div class="modal">
        <h3>{{ form.id ? "Шаблон задания" : "Новый шаблон" }}</h3>
        <div class="modal-body">
          <div class="fld-row">
            <label class="fld"><span>Этап</span>
              <select v-model="form.stage_code" class="input"><option v-for="s in STAGES" :key="s" :value="s">{{ s }}</option></select>
            </label>
            <label class="fld"><span>Форма</span>
              <select v-model="form.form_code" class="input"><option v-for="f in FORMS" :key="f" :value="f">{{ f }}</option></select>
            </label>
          </div>
          <label class="fld"><span>Название</span><input v-model="form.title" class="input" placeholder="Товарооборот розницы" /></label>
          <div class="fld-row">
            <label class="fld"><span>Роль</span>
              <select v-model="form.task_role" class="input"><option value="fill">Заполнение</option><option value="approve">Согласование</option></select>
            </label>
            <label class="fld"><span>Разрез (как делить)</span>
              <select v-model="form.group_by" class="input"><option value="position">По ТОПу (должности)</option><option value="legal_entity">По ЮЛ</option><option value="none">Одно задание</option></select>
            </label>
          </div>
          <div class="fld-sec">Фильтр ЦФО (какие ЦФО входят):</div>
          <div class="fld-row">
            <label class="fld"><span>Тип</span><input v-model="form.cfo_filter.entity_type" class="input" placeholder="отдел / магазин / производство" /></label>
            <label class="fld"><span>Группа</span><input v-model="form.cfo_filter.group_cfo1" class="input" placeholder="Розница / Производство…" /></label>
          </div>
          <div class="fld-row">
            <label class="fld"><span>Подгруппа</span><input v-model="form.cfo_filter.group_cfo2" class="input" /></label>
            <label class="fld"><span>Страна</span>
              <select v-model="form.cfo_filter.country" class="input"><option value="">любая</option><option value="BY">Беларусь</option><option value="RU">Россия</option><option value="KZ">Казахстан</option><option value="UZ">Узбекистан</option></select>
            </label>
          </div>
        </div>
        <div class="modal-foot">
          <button class="btn btn-ghost" @click="modal = false">Отмена</button>
          <button class="btn btn-primary" :disabled="!form.title" @click="save"><Icon name="lucide:check" /> Сохранить</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useTasks, type TaskTemplate, type CfoFilter } from "~/composables/useTasks";

definePageMeta({ middleware: "scope-guard" });

const api = useTasks();
const { hasRole, isAdmin } = useScope();
const canEdit = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));

const STAGES = ["1.1", "1.2", "1.3", "1.4", "1.5", "1.6", "2.1", "2.2", "2.3", "2.4", "3", "4"];
const FORMS = ["TPL-TO-RETAIL", "TPL-MP", "TPL-WHOLESALE", "TPL-IM", "TPL-CFO-EXP", "TPL-PROD-MINUTES", "TPL-LE-APPROVE"];

const templates = ref<TaskTemplate[]>([]);
const error = ref("");
const modal = ref(false);
const form = reactive<{ id: number; stage_code: string; form_code: string; title: string; task_role: string; group_by: string; cfo_filter: CfoFilter; sort_order: number }>({
  id: 0, stage_code: "1.1", form_code: "TPL-TO-RETAIL", title: "", task_role: "fill", group_by: "position", cfo_filter: {}, sort_order: 100
});

const stages = computed(() => [...new Set(templates.value.map((t) => t.stage_code))].sort());
const byStage = (s: string) => templates.value.filter((t) => t.stage_code === s);
const filterLabel = (f: CfoFilter) => {
  const p = Object.entries(f).filter(([, v]) => v).map(([k, v]) => `${k}=${v}`);
  return p.length ? p.join(", ") : "все ЦФО";
};
const groupLabel = (g: string) => ({ position: "по ТОПу", legal_entity: "по ЮЛ", none: "одно" }[g] ?? g);

const load = async () => { try { templates.value = await api.templates(); } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка"; } };
const openEdit = (t?: TaskTemplate) => {
  Object.assign(form, t ? { id: t.id, stage_code: t.stage_code, form_code: t.form_code, title: t.title, task_role: t.task_role, group_by: t.group_by, cfo_filter: { ...t.cfo_filter }, sort_order: t.sort_order }
    : { id: 0, stage_code: "1.1", form_code: "TPL-TO-RETAIL", title: "", task_role: "fill", group_by: "position", cfo_filter: {}, sort_order: 100 });
  modal.value = true;
};
const save = async () => { try { await api.saveTemplate({ ...form }); modal.value = false; await load(); } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка"; } };
const remove = async (t: TaskTemplate) => { try { await api.deleteTemplate(t.id); await load(); } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка"; } };

onMounted(load);
</script>

<style scoped>
.banner { padding: var(--sp-4) var(--sp-5); border-radius: var(--rd-4, 6px); margin-bottom: var(--sp-5); font-size: var(--fs-sm); }
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
.banner-warn { background: var(--warn-soft); color: var(--warn); }
.page-actions { display: flex; gap: var(--sp-3); }
.stage-group { margin-bottom: var(--sp-5); }
.sg-title { font-size: var(--fs-sm); color: var(--text-secondary); margin-bottom: var(--sp-3); text-transform: uppercase; letter-spacing: 0.03em; }
.tt-table { padding: var(--sp-2) var(--sp-4); }
.cell-code { font-family: var(--font-mono); font-size: var(--fs-xs); }
.td-filter { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-secondary); }
.th-act { width: 1%; white-space: nowrap; text-align: right; }
.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.32); display: flex; align-items: center; justify-content: center; z-index: 60; }
.modal { width: 520px; max-width: 94vw; background: var(--bg-surface); border-radius: var(--rd-5, 10px); box-shadow: 0 20px 60px rgba(0,0,0,0.25); padding: var(--sp-5); }
.modal h3 { margin: 0 0 var(--sp-4); font-size: var(--fs-md); }
.modal-body { display: flex; flex-direction: column; gap: var(--sp-3); }
.fld { display: flex; flex-direction: column; gap: 4px; }
.fld > span { font-size: var(--fs-xs); color: var(--text-muted); }
.fld-row { display: grid; grid-template-columns: 1fr 1fr; gap: var(--sp-3); }
.fld-sec { font-size: var(--fs-xs); color: var(--text-muted); margin-top: var(--sp-2); }
.modal-foot { display: flex; justify-content: flex-end; gap: var(--sp-3); margin-top: var(--sp-5); }
</style>
