<!--
  «Пользователи и права» модуля «Тактические планы» (ТЗ §«Разграничение прав»).
  Должность-центричная модель:
    • Должность привязана к этапам процесса; на должность назначаются пользователи
      (со срезом ABAC: этап × ЦФО × страна × ЮЛ).
    • Заместители: глобальный (на весь функционал) и в разрезе каждого этапа.
    • Метка отсутствия пользователя (отпуск/уволен) — глобально во всех интерфейсах;
      при срабатывании функционал подхватывает зам. Позже метка тянется из Bitrix24.
  Системные роли (Администратор / Администратор ТП / Участник) — отдельный гейт.
  Доступ — только системным админам (ROLE_PLANS_ADMIN / ROLE_ADMIN).
-->
<template>
  <div class="page-rights">
    <header class="page-header">
      <div>
        <h1 class="page-title">Пользователи и права</h1>
        <p class="page-subtitle">Должности процесса, назначения, замы и системные роли</p>
      </div>
      <NuxtLink to="/plans" class="btn btn-ghost"><Icon name="lucide:arrow-left" /> К модулю</NuxtLink>
    </header>

    <p v-if="!isAdmin" class="banner banner-warn">Доступ к настройке прав — только администраторам Тактических планов.</p>
    <template v-else>
      <p v-if="error" class="banner banner-neg">{{ error }}</p>

      <div class="tabs">
        <button class="tab" :class="{ active: tab === 'positions' }" @click="tab = 'positions'">Роли в процессе и назначения</button>
        <button class="tab" :class="{ active: tab === 'people' }" @click="tab = 'people'">Пользователи, замы, отсутствия</button>
      </div>

      <!-- ВКЛАДКА 1: ДОЛЖНОСТИ -->
      <section v-if="tab === 'positions'" class="positions">
        <div class="pos-toolbar">
          <p class="hint">Роль в процессе (заполняет / согласует) привязана к этапам; на неё назначаются сотрудники со срезом. Реальные должности (IT-Директор и т.п.) — на странице <NuxtLink to="/plans/admin/positions" class="rr-link">Должности</NuxtLink>.</p>
          <button class="btn btn-sm btn-primary" @click="openPosition()"><Icon name="lucide:plus" /> Роль</button>
        </div>

        <div v-for="p in positions" :key="p.code" class="card pos-block">
          <div class="pb-head">
            <div class="pb-title">
              <span class="badge" :class="trackTone(p.track)">{{ trackLabel(p.track) }}</span>
              <b class="pb-name">{{ p.name }}</b>
              <span class="cell-code">{{ p.code }}</span>
            </div>
            <div class="pb-actions">
              <button class="btn btn-icon btn-sm" title="Изменить" @click="openPosition(p)"><Icon name="lucide:pencil" /></button>
              <button class="btn btn-icon btn-sm" title="Удалить" @click="removePosition(p.code)"><Icon name="lucide:trash-2" /></button>
            </div>
          </div>
          <p class="pb-desc">{{ p.description }}</p>

          <!-- Этапы процесса -->
          <div class="pb-stages">
            <span class="pb-label">Этапы процесса:</span>
            <button v-for="st in STAGES" :key="st" class="stage-pill" :class="{ on: p.stage_codes.includes(st) }" @click="toggleStage(p, st)">{{ st }}</button>
          </div>

          <!-- Назначенные сотрудники -->
          <div class="pb-users">
            <div class="pb-label-row">
              <span class="pb-label">Назначенные сотрудники ({{ posUsers(p.code).length }}):</span>
              <button class="btn btn-sm btn-ghost" @click="openAssign(p)"><Icon name="lucide:user-plus" /> Назначить</button>
            </div>
            <table v-if="posUsers(p.code).length" class="data-table compact">
              <tbody>
                <tr v-for="a in posUsers(p.code)" :key="a.id">
                  <td class="u-cell">
                    {{ a.user_name }}
                    <span v-if="absenceOf(a.user_id)" class="badge badge-warn abs-tag"><Icon name="lucide:plane" /> {{ absenceLabel(absenceOf(a.user_id)) }}</span>
                  </td>
                  <td class="scope-cell">{{ scopeLabel(a) }}</td>
                  <td class="th-act"><button class="ac-del" title="Снять" @click="removeAssign(a.id)"><Icon name="lucide:x" /></button></td>
                </tr>
              </tbody>
            </table>
            <p v-else class="u-none">Никто не назначен.</p>
          </div>
        </div>
      </section>

      <!-- ВКЛАДКА 2: ПОЛЬЗОВАТЕЛИ / ЗАМЫ / ОТСУТСТВИЯ -->
      <section v-else class="people">
        <!-- Прозрачная догрузка: поиск по фамилии (B24 + локальные), выбор добавляет -->
        <div class="card import-card">
          <div class="ic-head">
            <div>
              <b>Добавить сотрудника</b>
              <span class="ic-hint">Начните вводить фамилию — найдём в Bitrix24 (тот же вход) и в системе; выбор добавит, даже если он ещё не заходил</span>
            </div>
          </div>
          <ClientOnly>
            <UserPicker placeholder="Фамилия сотрудника…" @picked="onUserPicked" />
          </ClientOnly>
          <p v-if="addedNote" class="ic-added"><Icon name="lucide:check" /> {{ addedNote }}</p>
        </div>

        <div class="card people-card">
        <div class="legend">
          <span class="legend-i"><span class="badge badge-accent">Администратор</span> — глобальный</span>
          <span class="legend-i"><span class="badge badge-info">Администратор ТП</span> — назначает остальных</span>
          <span class="legend-i"><span class="badge badge-pos">Участник</span></span>
          <span class="legend-i"><span class="badge badge-warn">метка</span> — отсутствие → подхватывает зам</span>
        </div>
        <div class="table-wrap">
          <table class="data-table">
            <thead>
              <tr><th>Сотрудник</th><th>Системная роль</th><th>Отсутствие</th><th>Глобальный зам</th></tr>
            </thead>
            <tbody>
              <tr v-for="u in users" :key="u.id" :class="{ 'row-absent': u.absence_status }">
                <td>
                  <div class="u-name">{{ u.last_name }} {{ u.name }}</div>
                  <div class="u-email">{{ u.email }}</div>
                </td>
                <td>
                  <span v-if="u.global_admin" class="badge badge-accent">Администратор</span>
                  <select v-else class="input input-sm" :value="sysRole(u)" @change="setSysRole(u, ($event.target as HTMLSelectElement).value)">
                    <option value="">Нет доступа</option>
                    <option value="user">Участник</option>
                    <option value="admin">Администратор ТП</option>
                  </select>
                </td>
                <td>
                  <div class="abs-cell">
                    <select class="input input-sm" :value="u.absence_status" @change="setAbs(u, ($event.target as HTMLSelectElement).value)">
                      <option value="">на месте</option>
                      <option value="vacation">отпуск</option>
                      <option value="sick">больничный</option>
                      <option value="dismissed">уволен</option>
                    </select>
                    <span v-if="u.absence_until" class="abs-until">до {{ u.absence_until }}</span>
                  </div>
                </td>
                <td>
                  <div v-if="globalDeputyName(u.id)" class="dep-set">
                    <Icon name="lucide:user-check" class="ds-ic" /> {{ globalDeputyName(u.id) }}
                    <button class="ac-del" title="Снять зама" @click="setDeputy(u.id, '', '')"><Icon name="lucide:x" /></button>
                  </div>
                  <ClientOnly v-else>
                    <UserPicker placeholder="зам…" @picked="(d) => setDeputy(u.id, '', String(d.id))" />
                  </ClientOnly>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        </div>
      </section>
    </template>

    <!-- МОДАЛ: назначить сотрудника на должность -->
    <div v-if="assignModal" class="modal-overlay" @click.self="assignModal = null">
      <div class="modal">
        <h3>Назначить на должность · {{ assignModal.name }}</h3>
        <div class="modal-body">
          <label class="fld"><span>Сотрудник</span>
            <div v-if="aForm.user_id" class="picked-user">
              <Icon name="lucide:user-check" /> {{ aForm.user_name }}
              <button class="ac-del" @click="aForm.user_id = 0"><Icon name="lucide:x" /></button>
            </div>
            <ClientOnly v-else>
              <UserPicker placeholder="Фамилия сотрудника…" @picked="onAssignPicked" />
            </ClientOnly>
          </label>
          <div class="fld-row">
            <label class="fld"><span>Этап</span>
              <select v-model="aForm.stage_code" class="input">
                <option value="">все этапы должности</option>
                <option v-for="st in assignModal.stage_codes" :key="st" :value="st">{{ st }}</option>
              </select>
            </label>
            <label class="fld"><span>Страна</span>
              <select v-model="aForm.country" class="input">
                <option value="">все</option>
                <option v-for="c in COUNTRIES" :key="c.code" :value="c.code">{{ c.name }}</option>
              </select>
            </label>
          </div>
          <label class="fld"><span>Юридическое лицо</span>
            <select v-model="aForm.legal_entity" class="input">
              <option value="">все ЮЛ</option>
              <option v-for="le in legalEntities" :key="le" :value="le">{{ le }}</option>
            </select>
          </label>
          <label class="fld"><span>ЦФО (задания) — из справочника</span>
            <ClientOnly>
              <CfoPicker v-model="aForm.codesArr" :country="aForm.country" />
            </ClientOnly>
          </label>
        </div>
        <div class="modal-foot">
          <button class="btn btn-ghost" @click="assignModal = null">Отмена</button>
          <button class="btn btn-primary" :disabled="!aForm.user_id" @click="saveAssign"><Icon name="lucide:check" /> Назначить</button>
        </div>
      </div>
    </div>


    <!-- МОДАЛ: должность -->
    <div v-if="posModal" class="modal-overlay" @click.self="posModal = null">
      <div class="modal">
        <h3>{{ posEditing ? "Должность" : "Новая должность" }}</h3>
        <div class="modal-body">
          <div class="fld-row">
            <label class="fld"><span>Код</span>
              <input v-model="pForm.code" class="input" :disabled="posEditing" placeholder="latin_snake" />
            </label>
            <label class="fld"><span>Трек</span>
              <select v-model="pForm.track" class="input">
                <option value="sales">Продажи</option>
                <option value="production">Производство</option>
                <option value="final">Финал</option>
                <option value="nsi">НСИ / прочее</option>
              </select>
            </label>
          </div>
          <label class="fld"><span>Название</span><input v-model="pForm.name" class="input" /></label>
          <label class="fld"><span>Назначение</span><input v-model="pForm.description" class="input" /></label>
        </div>
        <div class="modal-foot">
          <button class="btn btn-ghost" @click="posModal = null">Отмена</button>
          <button class="btn btn-primary" :disabled="!pForm.code || !pForm.name" @click="savePosition"><Icon name="lucide:check" /> Сохранить</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { usePlansRights, type Position, type ScopeAssignment, type PlanUser, type Deputy } from "~/composables/usePlansRights";
import { useDirectories } from "~/composables/useDirectories";
import UserPicker from "~/components/plans/UserPicker.vue";
import CfoPicker from "~/components/plans/CfoPicker.vue";

definePageMeta({ middleware: "scope-guard" });

const rights = usePlansRights();
const { hasRole } = useScope();
const isAdmin = computed(() => hasRole("ROLE_PLANS_ADMIN") || hasRole("ROLE_ADMIN"));

const STAGES = ["1.1", "1.2", "1.3", "1.4", "1.5", "1.6", "2.1", "2.2", "2.3", "2.4", "3", "4"];
const COUNTRIES = [
  { code: "BY", name: "Беларусь" }, { code: "RU", name: "Россия" },
  { code: "KZ", name: "Казахстан" }, { code: "UZ", name: "Узбекистан" }
];

const tab = ref<"positions" | "people">("positions");
const users = ref<PlanUser[]>([]);
const positions = ref<Position[]>([]);
const assignments = ref<ScopeAssignment[]>([]);
const deputies = ref<Deputy[]>([]);
const error = ref("");

const assignModal = ref<Position | null>(null);
const aForm = reactive({ user_id: 0, user_name: "", stage_code: "", country: "", legal_entity: "", codesArr: [] as number[] });
const legalEntities = ref<string[]>([]);
const onAssignPicked = async (u: { id: number; name: string }) => {
  aForm.user_id = u.id;
  aForm.user_name = u.name;
  users.value = await rights.users().catch(() => users.value);
};
const addedNote = ref("");
const onUserPicked = async (u: { id: number; name: string }) => {
  addedNote.value = `${u.name} — добавлен в систему`;
  try {
    users.value = await rights.users();
  } catch (e) {
    error.value = errMsg(e);
  }
  setTimeout(() => (addedNote.value = ""), 4000);
};

const posModal = ref(false);
const posEditing = ref(false);
const pForm = reactive<Position>({ code: "", name: "", description: "", default_stage_code: "", track: "sales", sort_order: 100, stage_codes: [] });

const posUsers = (code: string) => assignments.value.filter((a) => a.role === code);
const others = (id: number) => users.value.filter((u) => u.id !== id);
const absenceOf = (id: number) => users.value.find((u) => u.id === id)?.absence_status ?? "";
const sysRole = (u: PlanUser) => (u.plans_admin ? "admin" : u.plans_user ? "user" : "");
const globalDeputyOf = (id: number) => deputies.value.find((d) => d.principal_user_id === id && d.stage_code === "")?.deputy_user_id ?? "";
const globalDeputyName = (id: number) => deputies.value.find((d) => d.principal_user_id === id && d.stage_code === "")?.deputy_name ?? "";

const scopeLabel = (a: ScopeAssignment) => {
  const parts: string[] = [];
  if (a.stage_code) parts.push(`этап ${a.stage_code}`);
  if (a.country) parts.push(a.country);
  if (a.legal_entity) parts.push(a.legal_entity);
  if (a.code_cfo?.length) parts.push(`ЦФО ${a.code_cfo.join(",")}`);
  return parts.length ? parts.join(" · ") : "весь срез";
};
const ABS: Record<string, string> = { vacation: "отпуск", sick: "больничный", dismissed: "уволен" };
const absenceLabel = (s: string) => ABS[s] ?? s;

const TRACK: Record<string, { l: string; t: string }> = {
  sales: { l: "продажи", t: "badge-pos" }, production: { l: "производство", t: "badge-info" },
  final: { l: "финал", t: "badge-accent" }, nsi: { l: "НСИ", t: "badge-warn" }
};
const trackLabel = (t: string) => TRACK[t]?.l ?? t;
const trackTone = (t: string) => TRACK[t]?.t ?? "";

const load = async () => {
  error.value = "";
  try {
    [users.value, positions.value, assignments.value, deputies.value] = await Promise.all([
      rights.users(), rights.positions(), rights.assignments(), rights.deputies()
    ]);
  } catch (e) { error.value = errMsg(e); }
};
const errMsg = (e: unknown) => (e instanceof Error ? e.message : "Ошибка");

const toggleStage = async (p: Position, st: string) => {
  const next = p.stage_codes.includes(st) ? p.stage_codes.filter((s) => s !== st) : [...p.stage_codes, st];
  p.stage_codes = next;
  try { await rights.setPositionStages(p.code, next); } catch (e) { error.value = errMsg(e); }
};

const setSysRole = async (u: PlanUser, val: string) => {
  try { await rights.setUserRoles(u.id, val === "admin", val === "user"); await load(); } catch (e) { error.value = errMsg(e); }
};
const setAbs = async (u: PlanUser, status: string) => {
  try { await rights.setAbsence(u.id, status, u.absence_until || ""); await load(); } catch (e) { error.value = errMsg(e); }
};
const setDeputy = async (principal: number, stage: string, deputyId: string) => {
  try {
    if (!deputyId) {
      const ex = deputies.value.find((d) => d.principal_user_id === principal && d.stage_code === stage);
      if (ex) await rights.deleteDeputy(ex.id);
    } else {
      await rights.saveDeputy({ principal_user_id: principal, deputy_user_id: Number(deputyId), stage_code: stage });
    }
    deputies.value = await rights.deputies();
  } catch (e) { error.value = errMsg(e); }
};


const openAssign = (p: Position) => { assignModal.value = p; Object.assign(aForm, { user_id: 0, user_name: "", stage_code: "", country: "", legal_entity: "", codesArr: [] }); };
const saveAssign = async () => {
  if (!assignModal.value || !aForm.user_id) return;
  try {
    await rights.assign(aForm.user_id, {
      role: assignModal.value.code, stage_code: aForm.stage_code,
      country: aForm.country, legal_entity: aForm.legal_entity, code_cfo: aForm.codesArr
    });
    assignModal.value = null;
    assignments.value = await rights.assignments();
  } catch (e) { error.value = errMsg(e); }
};
const removeAssign = async (id: number) => {
  try { await rights.unassign(id); assignments.value = await rights.assignments(); } catch (e) { error.value = errMsg(e); }
};

const openPosition = (p?: Position) => {
  posEditing.value = !!p;
  Object.assign(pForm, p ?? { code: "", name: "", description: "", default_stage_code: "", track: "sales", sort_order: 100, stage_codes: [] });
  posModal.value = true;
};
const savePosition = async () => {
  try { await rights.savePosition({ ...pForm }); posModal.value = false; positions.value = await rights.positions(); } catch (e) { error.value = errMsg(e); }
};
const removePosition = async (code: string) => {
  try { await rights.deletePosition(code); positions.value = await rights.positions(); } catch (e) { error.value = errMsg(e); }
};

const loadLegalEntities = async () => {
  try {
    const rows = await useDirectories().rows("dir_legal_entity");
    legalEntities.value = rows.map((r) => String(r.payload.name ?? "")).filter(Boolean);
  } catch {
    legalEntities.value = [];
  }
};

onMounted(() => { if (isAdmin.value) { load(); loadLegalEntities(); } });
</script>

<style scoped>
.banner { padding: var(--sp-4) var(--sp-5); border-radius: var(--rd-4, 6px); margin-bottom: var(--sp-5); font-size: var(--fs-sm); }
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
.banner-warn { background: var(--warn-soft); color: var(--warn); }

.tabs { display: flex; gap: var(--sp-2); border-bottom: 1px solid var(--border); margin-bottom: var(--sp-4); }
.tab { padding: var(--sp-3) var(--sp-4); border: none; background: none; cursor: pointer; color: var(--text-secondary); font-size: var(--fs-sm); border-bottom: 2px solid transparent; margin-bottom: -1px; }
.tab.active { color: var(--accent); border-bottom-color: var(--accent); font-weight: var(--fw-semibold); }

.positions { display: flex; flex-direction: column; gap: var(--sp-4); }
.pos-toolbar { display: flex; justify-content: space-between; align-items: center; }
.hint { color: var(--text-secondary); font-size: var(--fs-sm); margin: 0; }
.rr-link { color: var(--accent); }
.pos-block { padding: var(--sp-4) var(--sp-5); }
.pb-head { display: flex; justify-content: space-between; align-items: center; }
.pb-title { display: flex; align-items: center; gap: var(--sp-3); }
.pb-name { font-size: var(--fs-md); }
.cell-code { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); }
.pb-desc { color: var(--text-secondary); font-size: var(--fs-sm); margin: var(--sp-2) 0 var(--sp-3); }
.pb-stages { display: flex; align-items: center; gap: var(--sp-2); flex-wrap: wrap; margin-bottom: var(--sp-4); padding-bottom: var(--sp-4); border-bottom: 1px dashed var(--border); }
.pb-label { font-size: var(--fs-xs); color: var(--text-muted); margin-right: var(--sp-2); }
.stage-pill { font-family: var(--font-mono); font-size: var(--fs-2xs); padding: 2px 8px; border: 1px solid var(--border); border-radius: var(--rd-3); background: var(--bg-surface); color: var(--text-muted); cursor: pointer; }
.stage-pill.on { background: var(--accent-soft); color: var(--accent); border-color: var(--accent); font-weight: var(--fw-semibold); }
.pb-label-row { display: flex; justify-content: space-between; align-items: center; margin-bottom: var(--sp-2); }
.data-table.compact td { padding: var(--sp-2) var(--sp-3); }
.u-cell { display: flex; align-items: center; gap: var(--sp-2); }
.abs-tag { font-size: var(--fs-2xs); }
.scope-cell { color: var(--text-secondary); font-size: var(--fs-xs); }
.u-none { color: var(--text-muted); font-size: var(--fs-sm); }
.th-act { width: 1%; text-align: right; }
.ac-del { border: none; background: none; cursor: pointer; color: var(--text-muted); display: inline-flex; padding: 2px; border-radius: 3px; }
.ac-del:hover { color: var(--neg-strong); background: var(--neg-soft); }

.people { display: flex; flex-direction: column; gap: var(--sp-4); }
.import-card { padding: var(--sp-4) var(--sp-5); }
.ic-head { display: flex; justify-content: space-between; align-items: flex-start; gap: var(--sp-4); margin-bottom: var(--sp-3); }
.ic-hint { display: block; color: var(--text-muted); font-size: var(--fs-xs); margin-top: 2px; }
.ic-added { display: flex; align-items: center; gap: var(--sp-2); margin: var(--sp-3) 0 0; font-size: var(--fs-xs); color: var(--pos-strong); }
.picked-user { display: flex; align-items: center; gap: var(--sp-2); padding: var(--sp-2) var(--sp-3); border: 1px solid var(--border); border-radius: var(--rd-4); font-size: var(--fs-sm); }
.picked-user .ac-del { margin-left: auto; }
.dep-set { display: flex; align-items: center; gap: var(--sp-2); font-size: var(--fs-sm); }
.dep-set .ds-ic { color: var(--pos-strong); }
.dep-set .ac-del { margin-left: auto; }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.people-card { padding: var(--sp-5); }
.legend { display: flex; flex-wrap: wrap; gap: var(--sp-4) var(--sp-5); margin-bottom: var(--sp-4); font-size: var(--fs-xs); color: var(--text-secondary); }
.legend-i { display: inline-flex; align-items: center; gap: var(--sp-2); }
.u-name { font-weight: var(--fw-semibold); font-size: var(--fs-sm); }
.u-email { color: var(--text-muted); font-size: var(--fs-2xs); }
.row-absent { background: var(--warn-soft); }
.abs-cell { display: flex; flex-direction: column; gap: 2px; }
.abs-until { font-size: var(--fs-2xs); color: var(--text-muted); }
.stage-deps { display: flex; flex-wrap: wrap; align-items: center; gap: var(--sp-2); }
.dep-chip { display: inline-flex; align-items: center; gap: var(--sp-2); background: var(--bg-tonal); border: 1px solid var(--border); border-radius: var(--rd-3); padding: 2px var(--sp-2); font-size: var(--fs-2xs); }

.modal-overlay { position: fixed; inset: 0; background: rgba(0,0,0,0.32); display: flex; align-items: center; justify-content: center; z-index: 60; }
.modal { width: 480px; max-width: 94vw; background: var(--bg-surface); border-radius: var(--rd-5, 10px); box-shadow: 0 20px 60px rgba(0,0,0,0.25); padding: var(--sp-5); }
.modal h3 { margin: 0 0 var(--sp-4); font-size: var(--fs-md); }
.modal-body { display: flex; flex-direction: column; gap: var(--sp-4); }
.fld { display: flex; flex-direction: column; gap: 4px; }
.fld > span { font-size: var(--fs-xs); color: var(--text-muted); }
.fld-row { display: grid; grid-template-columns: 1fr 1fr; gap: var(--sp-4); }
.modal-foot { display: flex; justify-content: flex-end; gap: var(--sp-3); margin-top: var(--sp-5); }
</style>
