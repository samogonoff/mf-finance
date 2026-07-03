<!--
  Должности (job positions). Визуально разбиты на блоки: Учредители / Директора
  направлений / Топы (kind). 1 должность = 1 носитель. Носитель + покрытие ЦФО
  (ТОП, живая связь) + ЗАМЫ ПО ЭТАПАМ (привязаны к должности; глобальный зам —
  у пользователя на «Пользователи и права»).
-->
<template>
  <div class="page-pos">
    <header class="page-header">
      <div>
        <h1 class="page-title">Должности</h1>
        <p class="page-subtitle">Носитель + покрытие ЦФО (ТОП) + замы по этапам. Глобальные замы — на «Пользователи и права».</p>
      </div>
      <div class="page-actions">
        <NuxtLink to="/plans/admin/users" class="btn btn-ghost"><Icon name="lucide:users" /> Глоб. замы</NuxtLink>
        <button v-if="canEdit" class="btn btn-sm btn-primary" @click="openEdit()"><Icon name="lucide:plus" /> Должность</button>
      </div>
    </header>

    <p v-if="!canEdit" class="banner banner-warn">Просмотр. Редактирование — у роли «Администратор ТП».</p>
    <p v-if="error" class="banner banner-neg">{{ error }}</p>

    <div v-for="b in BLOCKS" :key="b.kind" class="block">
      <h2 class="block-title"><Icon :name="b.icon" /> {{ b.label }} <span class="block-count">{{ byKind(b.kind).length }}</span></h2>
      <div class="pos-grid">
        <div v-for="p in byKind(b.kind)" :key="p.id" class="card pos">
          <div class="p-head">
            <b class="p-title">{{ p.title }}</b>
            <div class="p-act">
              <button v-if="canEdit" class="btn btn-icon btn-sm" @click="openEdit(p)"><Icon name="lucide:pencil" /></button>
              <button v-if="canEdit" class="btn btn-icon btn-sm" @click="remove(p)"><Icon name="lucide:trash-2" /></button>
            </div>
          </div>
          <p v-if="p.description" class="p-desc">{{ p.description }}</p>

          <div class="p-row">
            <span class="p-lbl">Носитель</span>
            <div v-if="p.holder_name" class="p-holder">
              <Icon name="lucide:user-check" class="ph-ic" /> {{ p.holder_name }}
              <button v-if="canEdit" class="btn btn-icon btn-sm" title="Сменить" @click="setHolder(p, 0)"><Icon name="lucide:x" /></button>
            </div>
            <ClientOnly v-else>
              <UserPicker v-if="canEdit" placeholder="назначить носителя…" @picked="(u) => setHolder(p, u.id)" />
              <span v-else class="p-muted">не назначен</span>
            </ClientOnly>
          </div>

          <!-- Замы по этапам (привязаны к должности через носителя) -->
          <div v-if="p.holder_user_id" class="p-row">
            <span class="p-lbl">Замы по этапам</span>
            <div class="stage-deps">
              <span v-for="d in stageDepsFor(p.holder_user_id)" :key="d.id" class="dep-chip">
                <b>{{ d.stage_code }}</b> → {{ d.deputy_name }}
                <button v-if="canEdit" class="ac-del" @click="delDeputy(d.id)"><Icon name="lucide:x" /></button>
              </span>
              <button v-if="canEdit" class="btn btn-icon btn-sm" title="Зам по этапу" @click="openStageDep(p)"><Icon name="lucide:plus" /></button>
              <span v-if="!stageDepsFor(p.holder_user_id).length && !canEdit" class="p-muted">—</span>
            </div>
          </div>

          <div class="p-meta">
            <span class="chip"><Icon name="lucide:user" /> глоб. зам: {{ globalDepName(p.holder_user_id) || "—" }}</span>
            <span class="chip cfo" @click="canEdit && openCfo(p)"><Icon name="lucide:building-2" /> ЦФО: {{ p.cfo_count }}<Icon v-if="canEdit" name="lucide:settings-2" /></span>
          </div>
        </div>
        <p v-if="!byKind(b.kind).length" class="block-empty">Нет должностей этого типа.</p>
      </div>
    </div>

    <!-- МОДАЛ: должность -->
    <div v-if="editModal" class="modal-overlay" @click.self="editModal = false">
      <div class="modal">
        <h3>{{ eForm.id ? "Должность" : "Новая должность" }}</h3>
        <div class="modal-body">
          <label class="fld"><span>Название</span><input v-model="eForm.title" class="input" placeholder="IT-Директор" /></label>
          <label class="fld"><span>Тип</span>
            <select v-model="eForm.kind" class="input">
              <option value="founder">Учредитель</option>
              <option value="director">Директор направления</option>
              <option value="top">Топ (покрывает ЦФО)</option>
            </select>
          </label>
          <label class="fld"><span>Описание</span><input v-model="eForm.description" class="input" /></label>
        </div>
        <div class="modal-foot">
          <button class="btn btn-ghost" @click="editModal = false">Отмена</button>
          <button class="btn btn-primary" :disabled="!eForm.title" @click="saveEdit"><Icon name="lucide:check" /> Сохранить</button>
        </div>
      </div>
    </div>

    <!-- МОДАЛ: зам по этапу -->
    <div v-if="stageDepModal" class="modal-overlay" @click.self="stageDepModal = null">
      <div class="modal">
        <h3>Зам по этапу · {{ stageDepModal.title }}</h3>
        <div class="modal-body">
          <label class="fld"><span>Этап</span>
            <select v-model="dForm.stage_code" class="input">
              <option value="" disabled>этап…</option>
              <option v-for="st in STAGES" :key="st" :value="st">{{ st }}</option>
            </select>
          </label>
          <label class="fld"><span>Заместитель</span>
            <div v-if="dForm.deputy_user_id" class="p-holder">
              <Icon name="lucide:user-check" /> {{ dForm.deputy_name }}
              <button class="btn btn-icon btn-sm" @click="dForm.deputy_user_id = 0"><Icon name="lucide:x" /></button>
            </div>
            <ClientOnly v-else><UserPicker placeholder="Фамилия зама…" @picked="onDeputyPicked" /></ClientOnly>
          </label>
        </div>
        <div class="modal-foot">
          <button class="btn btn-ghost" @click="stageDepModal = null">Отмена</button>
          <button class="btn btn-primary" :disabled="!dForm.stage_code || !dForm.deputy_user_id" @click="saveStageDep"><Icon name="lucide:check" /> Назначить</button>
        </div>
      </div>
    </div>

    <!-- МОДАЛ: покрытие ЦФО -->
    <div v-if="cfoModal" class="modal-overlay" @click.self="cfoModal = null">
      <div class="modal modal-wide">
        <h3>Покрытие ЦФО · {{ cfoModal.title }}</h3>
        <div class="cur-cfo">
          <div class="cur-lbl">Закреплено ЦФО: <b>{{ currentCfo.length }}</b></div>
          <div v-if="currentCfo.length" class="cur-chips">
            <span v-for="c in currentCfo" :key="c" class="cur-chip">{{ cfoName(c) }}<button v-if="canEdit" class="cx" title="Снять" @click="unassign(c)"><Icon name="lucide:x" /></button></span>
          </div>
          <p v-else class="cur-empty">Пока ничего не закреплено.</p>
        </div>
        <div class="cfo-tabs">
          <button class="ct" :class="{ on: cfoTab === 'picker' }" @click="cfoTab = 'picker'">Точечно (пикер)</button>
          <button class="ct" :class="{ on: cfoTab === 'filter' }" @click="cfoTab = 'filter'">Массово (фильтр)</button>
        </div>
        <div v-if="cfoTab === 'picker'" class="modal-body">
          <p class="hint">Выберите ЦФО — они перейдут под эту должность (ТОП).</p>
          <ClientOnly><CfoPicker v-model="pickCodes" /></ClientOnly>
          <button class="btn btn-sm btn-primary" :disabled="!pickCodes.length" @click="applyPicker"><Icon name="lucide:check" /> Назначить {{ pickCodes.length }} ЦФО</button>
        </div>
        <div v-else class="modal-body">
          <p class="hint">Назначить все ЦФО под фильтр (напр. Тип=отдел + Подгруппа=Отдел IT).</p>
          <div class="filter-grid">
            <label class="fld"><span>Тип</span><select v-model="fForm.entity_type" class="input"><option value="">любой</option><option v-for="o in opts.type" :key="o" :value="o">{{ o }}</option></select></label>
            <label class="fld"><span>Группа</span><select v-model="fForm.group_cfo1" class="input"><option value="">любая</option><option v-for="o in opts.group" :key="o" :value="o">{{ o }}</option></select></label>
            <label class="fld"><span>Подгруппа</span><select v-model="fForm.group_cfo2" class="input"><option value="">любая</option><option v-for="o in opts.subgroup" :key="o" :value="o">{{ o }}</option></select></label>
            <label class="fld"><span>Страна</span><select v-model="fForm.country" class="input"><option value="">любая</option><option v-for="c in COUNTRIES" :key="c.code" :value="c.code">{{ c.name }}</option></select></label>
            <label class="fld"><span>ЮЛ</span><select v-model="fForm.legal_entity" class="input"><option value="">любое</option><option v-for="o in opts.le" :key="o" :value="o">{{ o }}</option></select></label>
            <label class="fld"><span>Сегмент МП</span><select v-model="fForm.segment" class="input"><option value="">любой</option><option value="large">large</option><option value="small">small</option></select></label>
          </div>
          <button class="btn btn-sm btn-primary" @click="applyFilter"><Icon name="lucide:filter" /> Применить фильтр</button>
        </div>
        <div v-if="assignNote" class="assign-note"><Icon name="lucide:check" /> {{ assignNote }}</div>
        <div class="modal-foot"><button class="btn btn-ghost" @click="cfoModal = null">Закрыть</button></div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useJobPositions, type JobPosition } from "~/composables/useJobPositions";
import { usePlansRights, type Deputy } from "~/composables/usePlansRights";
import { useDirectories } from "~/composables/useDirectories";
import UserPicker from "~/components/plans/UserPicker.vue";
import CfoPicker from "~/components/plans/CfoPicker.vue";

definePageMeta({ middleware: "scope-guard" });

const api = useJobPositions();
const rights = usePlansRights();
const { hasRole, isAdmin } = useScope();
const canEdit = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));

const BLOCKS = [
  { kind: "founder", label: "Учредители", icon: "lucide:crown" },
  { kind: "director", label: "Директора направлений", icon: "lucide:briefcase" },
  { kind: "top", label: "Топы", icon: "lucide:user-star" }
];
const STAGES = ["1.1", "1.2", "1.3", "1.4", "1.5", "1.6", "2.1", "2.2", "2.3", "2.4", "3", "4"];
const COUNTRIES = [
  { code: "BY", name: "Беларусь" }, { code: "RU", name: "Россия" },
  { code: "KZ", name: "Казахстан" }, { code: "UZ", name: "Узбекистан" }
];

const positions = ref<JobPosition[]>([]);
const deputies = ref<Deputy[]>([]);
const error = ref("");

const editModal = ref(false);
const eForm = reactive({ id: 0, title: "", kind: "top", description: "", holder_user_id: 0 });
const stageDepModal = ref<JobPosition | null>(null);
const dForm = reactive({ stage_code: "", deputy_user_id: 0, deputy_name: "" });

const cfoModal = ref<JobPosition | null>(null);
const cfoTab = ref<"picker" | "filter">("picker");
const pickCodes = ref<number[]>([]);
const fForm = reactive({ entity_type: "", group_cfo1: "", group_cfo2: "", country: "", legal_entity: "", segment: "" });
const assignNote = ref("");
const opts = reactive<{ type: string[]; group: string[]; subgroup: string[]; le: string[] }>({ type: [], group: [], subgroup: [], le: [] });
const currentCfo = ref<string[]>([]);
const cfoNames = ref<Record<string, string>>({});
const cfoName = (code: string) => cfoNames.value[code] ? `${cfoNames.value[code]} (${code})` : code;

const byKind = (k: string) => positions.value.filter((p) => p.kind === k);
const stageDepsFor = (holderId: number | null) => holderId ? deputies.value.filter((d) => d.principal_user_id === holderId && d.stage_code !== "") : [];
const globalDepName = (holderId: number | null) => holderId ? (deputies.value.find((d) => d.principal_user_id === holderId && d.stage_code === "")?.deputy_name ?? "") : "";

const errMsg = (e: unknown) => (e instanceof Error ? e.message : "Ошибка");
const load = async () => {
  try {
    [positions.value, deputies.value] = await Promise.all([api.list(), rights.deputies()]);
  } catch (e) { error.value = errMsg(e); }
};

const setHolder = async (p: JobPosition, userId: number) => {
  try { await api.upsert({ id: p.id, title: p.title, kind: p.kind, holder_user_id: userId, description: p.description }); await load(); } catch (e) { error.value = errMsg(e); }
};
const openEdit = (p?: JobPosition) => {
  Object.assign(eForm, p ? { id: p.id, title: p.title, kind: p.kind, description: p.description, holder_user_id: p.holder_user_id ?? 0 } : { id: 0, title: "", kind: "top", description: "", holder_user_id: 0 });
  editModal.value = true;
};
const saveEdit = async () => {
  try { await api.upsert({ id: eForm.id, title: eForm.title, kind: eForm.kind, holder_user_id: eForm.holder_user_id, description: eForm.description }); editModal.value = false; await load(); } catch (e) { error.value = errMsg(e); }
};
const remove = async (p: JobPosition) => { try { await api.remove(p.id); await load(); } catch (e) { error.value = errMsg(e); } };

const openStageDep = (p: JobPosition) => { stageDepModal.value = p; Object.assign(dForm, { stage_code: "", deputy_user_id: 0, deputy_name: "" }); };
const onDeputyPicked = async (d: { id: number; name: string }) => { dForm.deputy_user_id = d.id; dForm.deputy_name = d.name; deputies.value = await rights.deputies().catch(() => deputies.value); };
const saveStageDep = async () => {
  if (!stageDepModal.value?.holder_user_id) return;
  try {
    await rights.saveDeputy({ principal_user_id: stageDepModal.value.holder_user_id, deputy_user_id: dForm.deputy_user_id, stage_code: dForm.stage_code });
    stageDepModal.value = null;
    deputies.value = await rights.deputies();
  } catch (e) { error.value = errMsg(e); }
};
const delDeputy = async (id: number) => { try { await rights.deleteDeputy(id); deputies.value = await rights.deputies(); } catch (e) { error.value = errMsg(e); } };

const reloadCurrentCfo = async () => {
  if (!cfoModal.value) return;
  currentCfo.value = await api.cfo(cfoModal.value.id).catch(() => []);
};
const openCfo = async (p: JobPosition) => {
  cfoModal.value = p; cfoTab.value = "picker"; pickCodes.value = []; assignNote.value = "";
  Object.assign(fForm, { entity_type: "", group_cfo1: "", group_cfo2: "", country: "", legal_entity: "", segment: "" });
  currentCfo.value = [];
  await reloadCurrentCfo();
  if (!Object.keys(cfoNames.value).length) {
    try {
      const rows = await useDirectories().rows("dir_cfo");
      const m: Record<string, string> = {};
      for (const r of rows) m[String(r.payload.code_cfo ?? "")] = String(r.payload.name_cfo ?? "");
      cfoNames.value = m;
    } catch { /* имена необязательны */ }
  }
};
const unassign = async (code: string) => {
  try { await api.unassignCfo(code); await reloadCurrentCfo(); await load(); } catch (e) { error.value = errMsg(e); }
};
const applyPicker = async () => {
  if (!cfoModal.value) return;
  try { const r = await api.assignCfo(cfoModal.value.id, pickCodes.value); assignNote.value = `Назначено ${r.assigned} ЦФО`; pickCodes.value = []; await reloadCurrentCfo(); await load(); } catch (e) { error.value = errMsg(e); }
};
const applyFilter = async () => {
  if (!cfoModal.value) return;
  try { const r = await api.assignByFilter(cfoModal.value.id, { ...fForm }); assignNote.value = `Назначено ${r.assigned} ЦФО по фильтру`; await reloadCurrentCfo(); await load(); } catch (e) { error.value = errMsg(e); }
};

const loadOpts = async () => {
  try {
    const dirs = useDirectories();
    const [t, g, sg, le] = await Promise.all([dirs.rows("dir_cfo_type"), dirs.rows("dir_cfo_group"), dirs.rows("dir_cfo_subgroup"), dirs.rows("dir_legal_entity")]);
    opts.type = t.map((r) => String(r.payload.code ?? "")).filter(Boolean);
    opts.group = g.map((r) => String(r.payload.code ?? "")).filter(Boolean);
    opts.subgroup = sg.map((r) => String(r.payload.code ?? "")).filter(Boolean);
    opts.le = le.map((r) => String(r.payload.name ?? "")).filter(Boolean);
  } catch { /* опции необязательны */ }
};

onMounted(() => { load(); loadOpts(); });
</script>

<style scoped>
.banner { padding: var(--sp-4) var(--sp-5); border-radius: var(--rd-4, 6px); margin-bottom: var(--sp-5); font-size: var(--fs-sm); }
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
.banner-warn { background: var(--warn-soft); color: var(--warn); }
.page-actions { display: flex; gap: var(--sp-3); }

.block { margin-bottom: var(--sp-6); }
.block-title { display: flex; align-items: center; gap: var(--sp-2); font-size: var(--fs-sm); color: var(--text-secondary); margin-bottom: var(--sp-3); text-transform: uppercase; letter-spacing: 0.03em; }
.block-count { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); }
.block-empty { color: var(--text-muted); font-size: var(--fs-sm); }
.pos-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: var(--sp-4); }
.pos { padding: var(--sp-4) var(--sp-5); display: flex; flex-direction: column; gap: var(--sp-3); }
.p-head { display: flex; align-items: center; justify-content: space-between; }
.p-title { font-size: var(--fs-md); }
.p-desc { color: var(--text-secondary); font-size: var(--fs-sm); margin: 0; }
.p-row { display: flex; flex-direction: column; gap: var(--sp-2); }
.p-lbl { font-size: var(--fs-2xs); color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.03em; }
.p-holder { display: flex; align-items: center; gap: var(--sp-2); font-size: var(--fs-sm); }
.ph-ic { color: var(--pos-strong); }
.p-holder .btn-icon { margin-left: auto; }
.p-muted { color: var(--text-muted); font-size: var(--fs-sm); }
.stage-deps { display: flex; flex-wrap: wrap; align-items: center; gap: var(--sp-2); }
.dep-chip { display: inline-flex; align-items: center; gap: var(--sp-2); background: var(--bg-tonal); border: 1px solid var(--border); border-radius: var(--rd-3); padding: 2px var(--sp-2); font-size: var(--fs-2xs); }
.ac-del { border: none; background: none; cursor: pointer; color: var(--text-muted); display: inline-flex; padding: 0; }
.ac-del:hover { color: var(--neg-strong); }
.p-meta { display: flex; flex-wrap: wrap; gap: var(--sp-2); padding-top: var(--sp-3); border-top: 1px solid var(--border); }
.chip { display: inline-flex; align-items: center; gap: 4px; font-size: var(--fs-2xs); color: var(--text-secondary); background: var(--bg-tonal); border-radius: var(--rd-3); padding: 2px var(--sp-2); }
.chip.cfo { cursor: pointer; background: var(--accent-soft); color: var(--accent); }

.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.32); display: flex; align-items: center; justify-content: center; z-index: 60; }
.modal { width: 460px; max-width: 94vw; background: var(--bg-surface); border-radius: var(--rd-5, 10px); box-shadow: 0 20px 60px rgba(0,0,0,0.25); padding: var(--sp-5); }
.modal-wide { width: 560px; }
.modal h3 { margin: 0 0 var(--sp-4); font-size: var(--fs-md); }
.modal-body { display: flex; flex-direction: column; gap: var(--sp-4); }
.fld { display: flex; flex-direction: column; gap: 4px; }
.fld > span { font-size: var(--fs-xs); color: var(--text-muted); }
.modal-foot { display: flex; justify-content: flex-end; gap: var(--sp-3); margin-top: var(--sp-5); }
.cur-cfo { margin-bottom: var(--sp-4); padding-bottom: var(--sp-3); border-bottom: 1px dashed var(--border); }
.cur-lbl { font-size: var(--fs-xs); color: var(--text-muted); margin-bottom: var(--sp-2); }
.cur-chips { display: flex; flex-wrap: wrap; gap: var(--sp-2); max-height: 160px; overflow-y: auto; }
.cur-chip { display: inline-flex; align-items: center; gap: 4px; background: var(--accent-soft); color: var(--accent); border-radius: var(--rd-3); padding: 2px var(--sp-2); font-size: var(--fs-2xs); }
.cur-chip .cx { border: none; background: none; cursor: pointer; color: inherit; display: inline-flex; padding: 0; }
.cur-empty { font-size: var(--fs-xs); color: var(--text-muted); margin: 0; }
.cfo-tabs { display: flex; gap: var(--sp-2); border-bottom: 1px solid var(--border); margin-bottom: var(--sp-4); }
.ct { padding: var(--sp-2) var(--sp-3); border: none; background: none; cursor: pointer; color: var(--text-secondary); font-size: var(--fs-sm); border-bottom: 2px solid transparent; margin-bottom: -1px; }
.ct.on { color: var(--accent); border-bottom-color: var(--accent); font-weight: var(--fw-semibold); }
.hint { color: var(--text-secondary); font-size: var(--fs-sm); margin: 0; }
.filter-grid { display: grid; grid-template-columns: 1fr 1fr; gap: var(--sp-3); }
.assign-note { display: flex; align-items: center; gap: var(--sp-2); margin-top: var(--sp-3); color: var(--pos-strong); font-size: var(--fs-sm); }
</style>
