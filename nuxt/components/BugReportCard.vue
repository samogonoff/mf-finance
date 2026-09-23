<template>
  <Teleport to="body">
    <div class="bt-overlay" @click="$emit('close')" />
    <aside class="bt-card" role="dialog" aria-modal="true" :aria-label="`Репорт #${r.id}`">
      <header class="bt-head">
        <div class="bt-head-title">
          <span class="bt-id">#{{ r.id }}</span>
          <span class="badge badge-accent">{{ btLabel(BT_SECTION_LABELS, r.section) }}</span>
          <span class="badge" :class="btStatusBadge(r.status)">{{ btLabel(BT_STATUS_LABELS, r.status) }}</span>
        </div>
        <button class="btn btn-ghost btn-sm" aria-label="Закрыть" @click="$emit('close')">
          <Icon name="lucide:x" />
        </button>
      </header>

      <div class="bt-body">
        <h3 class="bt-title">{{ r.title }}</h3>

        <!-- Управление -->
        <section class="bt-section">
          <h4 class="bt-section-title">Статус</h4>
          <div class="bt-row">
            <select v-model="form.status" class="select">
              <option v-for="(label, key) in BT_STATUS_LABELS" :key="key" :value="key">{{ label }}</option>
            </select>
            <select v-if="form.status === 'resolved'" v-model.number="form.sourceId" class="select">
              <option :value="0">Источник проблемы *</option>
              <option v-for="s in sources" :key="s.id" :value="s.id">{{ s.name }}</option>
            </select>
            <button v-if="form.status === 'resolved'" type="button" class="btn btn-ghost btn-sm"
                    title="Добавить источник" @click="showAddSource = !showAddSource">
              <Icon name="lucide:plus" />
            </button>
          </div>
          <!-- Инлайн-создание источника: пустой справочник иначе не даёт закрыть баг. -->
          <div v-if="form.status === 'resolved' && showAddSource" class="bt-row">
            <input v-model="newSourceName" class="input" placeholder="Название нового источника"
                   maxlength="128" @keyup.enter="createSource" />
            <button type="button" class="btn btn-primary btn-sm"
                    :disabled="addingSource || !newSourceName.trim()" @click="createSource">
              <Icon :name="addingSource ? 'lucide:loader-2' : 'lucide:plus'" :class="{ spin: addingSource }" />
              Добавить
            </button>
          </div>

          <label class="bt-field">
            <span class="bt-label">ID задачи в Битрикс24</span>
            <input v-model="form.b24TaskId" class="input" placeholder="—" />
          </label>
          <label class="bt-field">
            <span class="bt-label">Комментарий администратора</span>
            <textarea v-model="form.adminComment" class="textarea" rows="3" />
          </label>

          <div class="bt-actions">
            <button class="btn btn-primary" :disabled="saving" @click="save">
              <Icon :name="saving ? 'lucide:loader-2' : 'lucide:save'" :class="{ spin: saving }" />
              Сохранить
            </button>
            <button class="btn btn-danger" :disabled="saving" @click="onDelete">
              <Icon name="lucide:trash-2" /> Удалить
            </button>
          </div>
          <div v-if="errorMessage" class="bt-error">{{ errorMessage }}</div>
          <div v-if="successMessage" class="bt-success">{{ successMessage }}</div>
        </section>

        <!-- Автоматический разбор -->
        <section v-if="r.claimed_by || r.mr_url" class="bt-section">
          <h4 class="bt-section-title">Автоматический разбор</h4>
          <dl class="bt-dl">
            <template v-if="r.claimed_by">
              <dt>Взял в работу</dt>
              <dd><code>{{ r.claimed_by }}</code> · {{ formatDate(r.claimed_at) }}</dd>
            </template>
            <template v-if="r.mr_url">
              <dt>Merge request</dt>
              <dd><a :href="r.mr_url" target="_blank" rel="noopener">!{{ r.mr_iid }}</a></dd>
            </template>
          </dl>
        </section>

        <!-- Описание -->
        <section class="bt-section">
          <h4 class="bt-section-title">Описание</h4>
          <p v-if="r.description" class="bt-text">{{ r.description }}</p>
          <p v-else class="bt-muted">—</p>
          <template v-if="r.steps">
            <div class="bt-label">Шаги воспроизведения</div>
            <pre class="bt-pre">{{ r.steps }}</pre>
          </template>
        </section>

        <!-- Автор и время -->
        <section class="bt-section">
          <h4 class="bt-section-title">Автор и время</h4>
          <dl class="bt-dl">
            <dt>Пользователь</dt>
            <dd>
              <template v-if="r.user">
                {{ r.user.name || r.user.email }}
                <span v-if="r.user.name" class="bt-muted">({{ r.user.email }})</span>
              </template>
              <span v-else class="bt-muted">автоматика</span>
            </dd>
            <dt>Тип</dt>
            <dd>{{ btLabel(BT_TYPE_LABELS, r.type) }}</dd>
            <dt>Создан</dt>
            <dd class="num">{{ formatDate(r.created_at) }}</dd>
            <dt>Обновлён</dt>
            <dd class="num">{{ formatDate(r.updated_at) }}</dd>
            <template v-if="r.resolved_at">
              <dt>Решён</dt>
              <dd class="num">{{ formatDate(r.resolved_at) }}</dd>
            </template>
            <template v-if="r.source">
              <dt>Причина</dt>
              <dd>{{ r.source.name }}</dd>
            </template>
            <dt>Сигнатура</dt>
            <dd><code class="bt-sig">{{ r.signature }}</code></dd>
          </dl>
        </section>

        <!-- Скриншоты -->
        <section v-if="r.screenshots.length" class="bt-section">
          <h4 class="bt-section-title">Скриншоты ({{ r.screenshots.length }})</h4>
          <div class="bt-shots">
            <a v-for="(name, i) in r.screenshots" :key="i" :href="shotUrl(name)" target="_blank" rel="noopener">
              <img :src="shotUrl(name)" alt="" loading="lazy" />
            </a>
          </div>
        </section>

        <!-- Страница -->
        <section class="bt-section">
          <h4 class="bt-section-title">Страница</h4>
          <dl class="bt-dl">
            <dt>URL</dt>
            <dd>
              <a v-if="r.url" :href="r.url" target="_blank" rel="noopener" class="bt-break">{{ r.url }}</a>
              <span v-else class="bt-muted">—</span>
            </dd>
            <template v-if="r.route?.name">
              <dt>Route</dt>
              <dd><code>{{ r.route.name }}</code></dd>
            </template>
            <template v-if="hasQuery">
              <dt>Параметры</dt>
              <dd><pre class="bt-pre">{{ formatJson(r.route.query) }}</pre></dd>
            </template>
          </dl>
        </section>

        <!-- Контекст страницы -->
        <section v-if="notEmpty(r.entity_ref) || notEmpty(r.context_snapshot)" class="bt-section">
          <h4 class="bt-section-title">Контекст страницы</h4>
          <details v-if="notEmpty(r.entity_ref)">
            <summary>Сущность</summary>
            <pre class="bt-pre">{{ formatJson(r.entity_ref) }}</pre>
          </details>
          <details v-if="notEmpty(r.context_snapshot)">
            <summary>Снимок состояния</summary>
            <pre class="bt-pre">{{ formatJson(r.context_snapshot) }}</pre>
          </details>
        </section>

        <section v-if="notEmpty(r.tech_context)" class="bt-section">
          <h4 class="bt-section-title">Технический контекст</h4>
          <details>
            <summary>Браузер / устройство</summary>
            <pre class="bt-pre">{{ formatJson(r.tech_context) }}</pre>
          </details>
        </section>

        <!-- Логи -->
        <section v-if="r.console_logs.length" class="bt-section">
          <h4 class="bt-section-title">Консоль ({{ r.console_logs.length }})</h4>
          <ul class="bt-log">
            <li v-for="(e, i) in r.console_logs" :key="i" :class="`bt-log--${e?.level}`">
              <span class="bt-log-level">{{ e?.level }}</span>
              <span class="bt-log-msg">{{ (e?.args || []).join(" ") }}</span>
            </li>
          </ul>
        </section>

        <section v-if="r.network_errors.length" class="bt-section">
          <h4 class="bt-section-title">
            Запросы к API ({{ r.network_errors.length }}, с ошибкой: {{ netErrorsCount }})
          </h4>
          <ul class="bt-net">
            <li v-for="(e, i) in r.network_errors" :key="i" :class="netClass(e)">
              <div class="bt-net-row" :class="{ clickable: hasNetDetails(e) }" @click="toggleNet(i)">
                <span class="bt-net-status">{{ e?.errorMessage ? "ERR" : (e?.status ?? "—") }}</span>
                <span class="bt-net-method">{{ e?.method }}</span>
                <span class="bt-net-url" :title="e?.url">{{ e?.url }}</span>
                <span class="bt-net-dur">{{ e?.durationMs }} мс</span>
                <Icon v-if="hasNetDetails(e)" :name="netOpen[i] ? 'lucide:chevron-down' : 'lucide:chevron-right'" />
              </div>
              <div v-if="hasNetDetails(e) && netOpen[i]" class="bt-net-details">
                <div v-if="e.errorMessage" class="bt-error">Ошибка fetch: {{ e.errorMessage }}</div>
                <template v-if="e.requestBody">
                  <div class="bt-label">Request body</div>
                  <pre class="bt-pre">{{ formatBody(e.requestBody) }}</pre>
                </template>
                <template v-if="e.responseBody">
                  <div class="bt-label">
                    Response body <span v-if="e.responseContentType" class="bt-muted">({{ e.responseContentType }})</span>
                  </div>
                  <pre class="bt-pre">{{ formatBody(e.responseBody) }}</pre>
                </template>
                <div v-if="e.requestId" class="bt-muted">X-Request-Id: <code>{{ e.requestId }}</code></div>
              </div>
            </li>
          </ul>
        </section>

        <section v-if="r.js_errors.length" class="bt-section">
          <h4 class="bt-section-title">JS-ошибки ({{ r.js_errors.length }})</h4>
          <details v-for="(e, i) in r.js_errors" :key="i">
            <summary>{{ e?.message }}</summary>
            <pre class="bt-pre">{{ e?.stack || "(нет stacktrace)" }}</pre>
          </details>
        </section>
      </div>
    </aside>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import {
  useBugTrackerAdmin, BT_STATUS_LABELS, BT_TYPE_LABELS, BT_SECTION_LABELS, btStatusBadge, btLabel,
  type BugReport, type BugSource
} from "~/composables/useBugTrackerAdmin";

const props = defineProps<{ report: BugReport; sources: BugSource[] }>();
const emit = defineEmits<{
  (e: "close"): void;
  (e: "updated", r: BugReport): void;
  (e: "deleted", id: number): void;
  (e: "sources-changed"): void;
}>();

const api = useBugTrackerAdmin();
const apiBase = useRuntimeConfig().public.apiBase;

const r = computed(() => props.report);
const saving = ref(false);
const errorMessage = ref<string | null>(null);
const successMessage = ref<string | null>(null);

const showAddSource = ref(false);
const newSourceName = ref("");
const addingSource = ref(false);

const formFrom = (rep: BugReport) => ({
  status: rep.status,
  sourceId: rep.source?.id ?? 0,
  b24TaskId: rep.b24_task_id ?? "",
  adminComment: rep.admin_comment ?? ""
});
const form = ref(formFrom(r.value));

// Другой репорт — сбрасываем форму. После сохранения id тот же, и форма
// остаётся как есть (там уже сохранённые значения).
watch(() => props.report.id, () => {
  form.value = formFrom(r.value);
  errorMessage.value = null;
  successMessage.value = null;
  showAddSource.value = false;
  newSourceName.value = "";
});

const hasQuery = computed(() => {
  const q = r.value.route?.query as Record<string, unknown> | undefined;
  return !!q && Object.keys(q).length > 0;
});
const notEmpty = (v: Record<string, any> | null | undefined) => !!v && Object.keys(v).length > 0;

const netOpen = reactive<Record<number, boolean>>({});
const toggleNet = (i: number) => { netOpen[i] = !netOpen[i]; };
const netClass = (e: any) => {
  if (e?.errorMessage || e?.status == null || e.status >= 500) return "bt-net--err";
  if (e.status >= 400) return "bt-net--warn";
  return "bt-net--ok";
};
const hasNetDetails = (e: any) => !!(e?.requestBody || e?.responseBody || e?.errorMessage || e?.requestId);
const netErrorsCount = computed(() => r.value.network_errors.filter((e: any) => !e?.ok).length);

const shotUrl = (name: string) => `${apiBase}/uploads/bugtracker/${r.value.id}/${name}`;

async function save() {
  errorMessage.value = null;
  successMessage.value = null;
  if (form.value.status === "resolved" && !form.value.sourceId) {
    errorMessage.value = "Для статуса «Решён» обязателен источник проблемы";
    return;
  }
  saving.value = true;
  try {
    const body: Record<string, any> = {
      status: form.value.status,
      b24_task_id: String(form.value.b24TaskId ?? "").trim(),
      admin_comment: form.value.adminComment
    };
    if (form.value.status === "resolved") body.source_id = form.value.sourceId;
    const updated = await api.patch(r.value.id, body as any);
    emit("updated", updated);
    successMessage.value = "Сохранено";
    setTimeout(() => (successMessage.value = null), 1500);
  } catch (err: any) {
    errorMessage.value = err?.data?.error || err?.message || "Не удалось сохранить";
  } finally {
    saving.value = false;
  }
}

async function createSource() {
  const name = newSourceName.value.trim();
  if (!name || addingSource.value) return;
  addingSource.value = true;
  errorMessage.value = null;
  try {
    const s = await api.sourceCreate(name);
    emit("sources-changed");
    form.value.sourceId = s.id;
    newSourceName.value = "";
    showAddSource.value = false;
  } catch (err: any) {
    errorMessage.value = err?.data?.error || err?.message || "Не удалось создать источник";
  } finally {
    addingSource.value = false;
  }
}

async function onDelete() {
  if (!confirm(`Удалить репорт #${r.value.id}?`)) return;
  saving.value = true;
  try {
    await api.remove(r.value.id);
    emit("deleted", r.value.id);
  } catch (err: any) {
    errorMessage.value = err?.data?.error || err?.message || "Не удалось удалить";
  } finally {
    saving.value = false;
  }
}

function formatDate(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString("ru-RU");
}
function formatJson(v: unknown): string {
  try { return JSON.stringify(v, null, 2); } catch { return String(v); }
}
function formatBody(body: string): string {
  try { return JSON.stringify(JSON.parse(body), null, 2); } catch { return body; }
}
</script>

<style scoped>
.bt-overlay { position: fixed; inset: 0; background: var(--bg-overlay); z-index: var(--z-drawer); }
.bt-card {
  position: fixed; top: 0; right: 0; bottom: 0;
  width: 640px; max-width: 100vw;
  background: var(--bg-surface);
  border-left: 1px solid var(--border);
  z-index: calc(var(--z-drawer) + 1);
  display: flex; flex-direction: column;
}
.bt-head {
  display: flex; align-items: center; justify-content: space-between; gap: var(--sp-3);
  padding: var(--sp-4) var(--sp-5);
  border-bottom: 1px solid var(--border);
}
.bt-head-title { display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.bt-id { font-family: var(--font-mono); font-variant-numeric: tabular-nums; font-weight: var(--fw-semibold); color: var(--text-strong); }
.bt-body { flex: 1; overflow-y: auto; padding: var(--sp-5); }
.bt-title { margin: 0 0 var(--sp-4); font-size: var(--fs-lg); color: var(--text-strong); }

.bt-section { padding: var(--sp-4) 0; border-top: 1px solid var(--border); }
.bt-section:first-of-type { border-top: none; padding-top: 0; }
.bt-section-title {
  margin: 0 0 var(--sp-3);
  font-size: var(--fs-xs); font-weight: var(--fw-semibold);
  color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.06em;
}
.bt-row { display: flex; gap: var(--sp-3); align-items: center; margin-bottom: var(--sp-3); }
.bt-row .select, .bt-row .input { flex: 1; min-width: 0; }
.bt-field { display: block; margin-bottom: var(--sp-3); }
.bt-label { display: block; font-size: var(--fs-xs); color: var(--text-muted); margin-bottom: 4px; }
.input, .select, .textarea {
  width: 100%; padding: 6px 10px;
  border: 1px solid var(--border); border-radius: var(--rd-3);
  background: var(--bg-app); color: var(--text-strong); font: inherit;
}
.textarea { resize: vertical; }
.bt-actions { display: flex; gap: var(--sp-3); }

.bt-text { margin: 0 0 var(--sp-3); white-space: pre-wrap; color: var(--text-primary); }
.bt-muted { color: var(--text-muted); }
.bt-break { word-break: break-all; }
.bt-dl { display: grid; grid-template-columns: 140px 1fr; gap: var(--sp-2) var(--sp-4); margin: 0; font-size: var(--fs-sm); }
.bt-dl dt { color: var(--text-muted); }
.bt-dl dd { margin: 0; min-width: 0; }
.num { font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
.bt-sig { font-family: var(--font-mono); font-size: var(--fs-xs); word-break: break-all; }
.bt-pre {
  margin: var(--sp-2) 0 0; padding: var(--sp-3);
  background: var(--bg-surface-2); border-radius: var(--rd-3);
  font-family: var(--font-mono); font-size: var(--fs-xs);
  white-space: pre-wrap; word-break: break-word; max-height: 320px; overflow: auto;
}
details summary { cursor: pointer; font-size: var(--fs-sm); color: var(--text-secondary); margin: var(--sp-2) 0; }

.bt-shots { display: grid; grid-template-columns: repeat(3, 1fr); gap: var(--sp-3); }
.bt-shots a { display: block; border: 1px solid var(--border); border-radius: var(--rd-3); overflow: hidden; }
.bt-shots img { width: 100%; height: auto; display: block; }

.bt-log, .bt-net { list-style: none; margin: 0; padding: 0; font-size: var(--fs-xs); font-family: var(--font-mono); }
.bt-log li { display: flex; gap: var(--sp-3); padding: 4px 0; border-bottom: 1px solid var(--border); }
.bt-log-level { flex: 0 0 44px; text-transform: uppercase; font-weight: var(--fw-semibold); }
.bt-log--error .bt-log-level { color: var(--neg); }
.bt-log--warn .bt-log-level { color: var(--warn); }
.bt-log-msg { word-break: break-word; white-space: pre-wrap; }

.bt-net li { border-bottom: 1px solid var(--border); }
.bt-net-row { display: flex; gap: var(--sp-3); align-items: center; padding: 4px 0; }
.bt-net-row.clickable { cursor: pointer; }
.bt-net-status { flex: 0 0 36px; font-weight: var(--fw-semibold); font-variant-numeric: tabular-nums; }
.bt-net--ok .bt-net-status { color: var(--pos); }
.bt-net--warn .bt-net-status { color: var(--warn); }
.bt-net--err .bt-net-status { color: var(--neg); }
.bt-net-method { flex: 0 0 48px; color: var(--text-muted); }
.bt-net-url { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bt-net-dur { flex: 0 0 auto; color: var(--text-muted); font-variant-numeric: tabular-nums; }
.bt-net-details { padding: 0 0 var(--sp-3) var(--sp-5); font-family: var(--font-sans); }

.bt-error { margin-top: var(--sp-3); color: var(--neg-strong); background: var(--neg-soft); padding: 6px 10px; border-radius: var(--rd-3); font-size: var(--fs-sm); }
.bt-success { margin-top: var(--sp-3); color: var(--pos-strong); background: var(--pos-soft); padding: 6px 10px; border-radius: var(--rd-3); font-size: var(--fs-sm); }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
