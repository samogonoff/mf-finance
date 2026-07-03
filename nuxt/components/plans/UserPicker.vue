<!--
  Прозрачный пикер пользователя по фамилии (механика добавления людей).
  Вводишь фамилию → выпадающий список: сверху уже добавленные (локальная БД),
  ниже — кандидаты из Bitrix24 (тот же OAuth-app, что и авторизация). Выбор
  локального — отдаёт его id; выбор из B24 — догружает (upsert) и отдаёт новый id.
  Эмитит picked({ id, name }).

  Выпадашка телепортируется в <body> с position:fixed по координатам инпута —
  иначе её режут overflow/stacking карточек и модалок.
-->
<template>
  <div class="picker">
    <div ref="anchor" class="pk-input">
      <Icon name="lucide:search" class="pk-ico" />
      <input
        v-model="q"
        class="pk-field"
        :placeholder="placeholder || 'Фамилия…'"
        @input="onInput"
        @focus="onFocus"
        @blur="onBlur"
      />
      <Icon v-if="loading" name="lucide:loader-circle" class="pk-ico spin" />
    </div>

    <Teleport to="body">
      <div v-if="open && q.length >= 2" class="pk-drop" :style="dropStyle">
        <div v-if="local.length" class="pk-group">В системе</div>
        <button v-for="u in local" :key="'l' + u.id" type="button" class="pk-item" @mousedown.prevent="pickLocal(u)">
          <Icon name="lucide:user-check" class="pk-uic" />
          <span class="pk-name">{{ u.last_name }} {{ u.name }}</span>
          <span class="pk-meta">{{ u.email }}</span>
        </button>

        <div v-if="b24.length" class="pk-group">Из Bitrix24 — добавить</div>
        <button v-for="c in b24" :key="'b' + c.id" type="button" class="pk-item" @mousedown.prevent="pickB24(c)">
          <Icon name="lucide:user-plus" class="pk-uic pk-new" />
          <span class="pk-name">{{ c.last_name }} {{ c.name }}</span>
          <span class="pk-meta">{{ c.position || c.email }}</span>
        </button>

        <div v-if="!loading && !local.length && !b24.length" class="pk-empty">Ничего не найдено</div>
        <div v-if="b24error" class="pk-err">{{ b24error }}</div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { usePlansRights, type PlanUser, type B24Candidate } from "~/composables/usePlansRights";

defineProps<{ placeholder?: string }>();
const emit = defineEmits<{ picked: [{ id: number; name: string }] }>();

const rights = usePlansRights();
const anchor = ref<HTMLElement | null>(null);
const q = ref("");
const open = ref(false);
const loading = ref(false);
const local = ref<PlanUser[]>([]);
const b24 = ref<B24Candidate[]>([]);
const b24error = ref("");
const dropStyle = ref<Record<string, string>>({});
let timer: ReturnType<typeof setTimeout> | null = null;
let seq = 0;

const updatePos = () => {
  const el = anchor.value;
  if (!el) return;
  const r = el.getBoundingClientRect();
  dropStyle.value = {
    position: "fixed",
    top: `${r.bottom + 4}px`,
    left: `${r.left}px`,
    width: `${r.width}px`
  };
};

const onFocus = () => { open.value = true; updatePos(); };

const onInput = () => {
  open.value = true;
  updatePos();
  if (timer) clearTimeout(timer);
  if (q.value.trim().length < 2) {
    local.value = [];
    b24.value = [];
    return;
  }
  timer = setTimeout(run, 280);
};

const run = async () => {
  const term = q.value.trim();
  const my = ++seq;
  loading.value = true;
  b24error.value = "";
  const [loc, ext] = await Promise.allSettled([rights.searchLocal(term), rights.searchB24(term)]);
  if (my !== seq) return; // устаревший ответ
  local.value = loc.status === "fulfilled" ? loc.value : [];
  if (ext.status === "fulfilled") {
    const known = new Set(local.value.map((u) => `${u.last_name} ${u.name}`.toLowerCase()));
    b24.value = ext.value.filter((c) => !known.has(`${c.last_name} ${c.name}`.toLowerCase()));
  } else {
    b24.value = [];
    b24error.value = "B24-поиск недоступен (войдите через Bitrix24)";
  }
  loading.value = false;
  updatePos();
};

const reset = () => { q.value = ""; local.value = []; b24.value = []; open.value = false; };

const pickLocal = (u: PlanUser) => {
  emit("picked", { id: u.id, name: `${u.last_name} ${u.name}`.trim() });
  reset();
};
const pickB24 = async (c: B24Candidate) => {
  try {
    const res = await rights.upsertUser(c);
    emit("picked", { id: res.id, name: `${c.last_name} ${c.name}`.trim() });
    reset();
  } catch {
    b24error.value = "Не удалось добавить пользователя";
  }
};

const onBlur = () => { setTimeout(() => (open.value = false), 150); };

const onScroll = () => { if (open.value) updatePos(); };
onMounted(() => {
  window.addEventListener("scroll", onScroll, true);
  window.addEventListener("resize", onScroll);
});
onBeforeUnmount(() => {
  window.removeEventListener("scroll", onScroll, true);
  window.removeEventListener("resize", onScroll);
});
</script>

<style scoped>
.picker { position: relative; }
.pk-input { display: flex; align-items: center; gap: var(--sp-2); border: 1px solid var(--border); border-radius: var(--rd-4); padding: 0 var(--sp-3); background: var(--bg-surface); }
.pk-ico { color: var(--text-muted); flex-shrink: 0; }
.pk-field { border: none; background: none; outline: none; padding: 6px 0; font: inherit; font-size: var(--fs-sm); flex: 1; min-width: 0; color: var(--text-primary); }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>

<style>
/* Не scoped: выпадашка телепортируется в body, scoped-стили её не достанут. */
.pk-drop { z-index: 1000; background: var(--bg-surface); border: 1px solid var(--border); border-radius: var(--rd-4); box-shadow: 0 10px 30px rgba(0, 0, 0, 0.18); max-height: 300px; overflow-y: auto; padding: 4px; }
.pk-drop .pk-group { font-size: var(--fs-2xs); text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); padding: var(--sp-2) var(--sp-3) 2px; }
.pk-drop .pk-item { display: flex; align-items: center; gap: var(--sp-2); width: 100%; text-align: left; border: none; background: none; cursor: pointer; padding: var(--sp-2) var(--sp-3); border-radius: var(--rd-3); }
.pk-drop .pk-item:hover { background: var(--bg-tonal); }
.pk-drop .pk-uic { color: var(--text-secondary); flex-shrink: 0; }
.pk-drop .pk-new { color: var(--accent); }
.pk-drop .pk-name { font-size: var(--fs-sm); }
.pk-drop .pk-meta { margin-left: auto; font-size: var(--fs-2xs); color: var(--text-muted); padding-left: var(--sp-3); }
.pk-drop .pk-empty, .pk-drop .pk-err { padding: var(--sp-3); font-size: var(--fs-xs); color: var(--text-muted); }
.pk-drop .pk-err { color: var(--warn); }
</style>
