<!--
  Оболочка процесса карточки формы (ТЗ МП §2, SPEC §28.4). Один компонент на два
  места — карточку периода и саму форму: состояние процесса нельзя показывать
  по-разному в двух местах, иначе «на согласовании» в одном экране и «черновик»
  в другом расходятся у одного и того же человека.

  Что закрывает:
   - §2.2 статусы карточки человеческими метками (сервер отдаёт технические
     on_approval_1.2 / returned_to_1.1 — собираем из пары статус+шаг);
   - §2.3 возврат ТОЛЬКО с целевым шагом и обязательным комментарием, аннулированные
     решения не удаляются, а показываются зачёркнутыми;
   - §2.4 переоткрытие утверждённого периода — с причиной и только админу;
   - §28.5 публикация: сначала предпросмотр (dry-run), запись — админом; отчёт
     показывает pending/unmapped, то есть открытые вопросы BI (§12) видны человеку,
     а не спрятаны в логах сервера.
-->
<template>
  <section class="cpp card">
    <div class="cpp-head">
      <div class="cpp-id">
        <span class="cpp-title">{{ card?.title || "Карточка формы" }}</span>
        <span class="cpp-meta">
          <span class="cpp-form">{{ card?.form_code }}</span>
          <span v-if="card">· {{ card.year }}-{{ String(card.month).padStart(2, "0") }}</span>
          <span v-if="card?.country">· {{ card.country }}</span>
          <span v-if="card?.legal_entity">· {{ card.legal_entity }}</span>
          <span v-if="card">· версия {{ card.current_version }}</span>
        </span>
      </div>
      <div class="cpp-state">
        <span class="badge" :class="statusBadge">{{ statusText }}</span>
        <span v-if="card?.locked" class="cpp-lock" title="после утверждения период закрыт на запись для всех">
          <Icon name="lucide:lock" /> закрыт на запись
        </span>
        <span class="cpp-mode" :class="{ inv: card?.calc_mode === 'inverse' }">
          {{ card?.calc_mode === "inverse" ? "расчёт от условий" : "расчёт от сумм" }}
        </span>
      </div>
    </div>

    <p v-if="error" class="cpp-banner neg">{{ error }}</p>
    <p v-if="note" class="cpp-banner pos">{{ note }}</p>
    <p v-if="loading && !card" class="cpp-loading"><Icon name="lucide:loader-circle" class="spin" /> Загружаю состояние карточки…</p>

    <!-- ===== Где карточка сейчас и что дальше ===== -->
    <div v-if="card" class="cpp-flow">
      <div class="fl-step">
        <span class="fl-lbl">Текущий шаг</span>
        <span class="fl-val">{{ stepTitle(card.step_code) }}</span>
        <span v-if="currentStep?.responsible" class="fl-sub">{{ currentStep.responsible }}</span>
      </div>
      <Icon name="lucide:arrow-right" class="fl-arrow" />
      <div class="fl-step">
        <span class="fl-lbl">Следующий шаг</span>
        <span class="fl-val">{{ nextStep ? stepTitle(nextStep.step_code) : "маршрут пройден" }}</span>
        <span v-if="nextStep?.responsible" class="fl-sub">{{ nextStep.responsible }}</span>
      </div>
      <div class="fl-route">
        <span
          v-for="s in enabledRoute"
          :key="s.step_code"
          class="fl-chip"
          :class="{ on: s.step_code === card.step_code, done: isPassed(s.step_code) }"
          :title="s.responsible || s.step_name"
        >{{ s.step_code }}</span>
      </div>
    </div>

    <!-- ===== Действия по статусу ===== -->
    <div v-if="card" class="cpp-actions">
      <button
        v-if="canSubmitStep"
        class="btn btn-sm btn-primary"
        :disabled="busy || !canSubmit"
        :title="canSubmit ? 'Отправить форму на согласование' : submitBlockedHint"
        @click="act('submit')"
      ><Icon name="lucide:send" /> Отправить на согласование</button>
      <span v-if="canSubmitStep && !canSubmit" class="cpp-hint">{{ submitBlockedHint }}</span>

      <button
        v-if="canApproveStep"
        class="btn btn-sm btn-primary"
        :disabled="busy"
        @click="act('approve')"
      ><Icon name="lucide:check-check" /> Согласовать</button>

      <button
        v-if="canReturn"
        class="btn btn-sm btn-ghost"
        :disabled="busy"
        @click="openReturn"
      ><Icon name="lucide:corner-up-left" /> Вернуть</button>

      <button
        v-if="canReopen"
        class="btn btn-sm btn-ghost"
        :disabled="busy"
        title="Утверждённый период открывается только с причиной — она уходит в аудит"
        @click="reopen.open = true"
      ><Icon name="lucide:unlock" /> Переоткрыть период</button>

      <span class="cpp-sep"></span>

      <button class="btn btn-sm btn-ghost" :disabled="busy" @click="doPublish('dry_run')">
        <Icon name="lucide:eye" /> Публикация: предпросмотр
      </button>
      <button
        v-if="isPlansAdmin"
        class="btn btn-sm btn-ghost"
        :disabled="busy"
        title="Записать в приёмник Budgeting"
        @click="doPublish('write')"
      ><Icon name="lucide:upload-cloud" /> Опубликовать</button>
    </div>

    <!-- ===== Отчёт публикации ===== -->
    <div v-if="pub" class="cpp-pub">
      <div class="pub-head">
        <span class="pub-title">
          Публикация — {{ pub.mode === "write" ? "запись" : "предпросмотр (ничего не записано)" }}
        </span>
        <span class="badge" :class="pubBadge">{{ pubStatus }}</span>
        <span v-if="pub.target" class="pub-target">приёмник: {{ pub.target }}</span>
      </div>
      <div class="pub-grid">
        <div class="pub-cell"><span class="pub-lbl">Строк к записи</span><span class="pub-val">{{ num(pub.rows_total) }}</span></div>
        <div class="pub-cell"><span class="pub-lbl">Записано</span><span class="pub-val">{{ num(pub.rows_written) }}</span></div>
        <div class="pub-cell"><span class="pub-lbl">Σ введено</span><span class="pub-val">{{ num(pub.sum_input, 2) }}</span></div>
        <div class="pub-cell"><span class="pub-lbl">Σ в приёмнике</span><span class="pub-val">{{ num(pub.sum_target, 2) }}</span></div>
        <!-- Контроль ТЗ §28.5: расхождение обязано быть нулём, иначе публикацию нельзя считать состоявшейся. -->
        <div class="pub-cell"><span class="pub-lbl">Расхождение</span><span class="pub-val" :class="pub.diff === 0 ? 'ok' : 'bad'">{{ num(pub.diff, 2) }}</span></div>
      </div>
      <p v-if="pub.error" class="cpp-banner neg">{{ pub.error }}</p>

      <div v-if="pub.pending?.length" class="pub-list warn">
        <span class="pl-title"><Icon name="lucide:clock" /> Ожидает подтверждения BI ({{ pub.pending.length }})</span>
        <span class="pl-hint">Правило маппинга есть, но выключено — это открытые вопросы ТЗ §12 (какой «Параметр», агрегат или детализация, BYN-пара).</span>
        <ul><li v-for="(p, i) in pub.pending" :key="'p' + i">{{ p }}</li></ul>
      </div>
      <div v-if="pub.unmapped?.length" class="pub-list neg">
        <span class="pl-title"><Icon name="lucide:alert-triangle" /> Нет правила маппинга ({{ pub.unmapped.length }})</span>
        <span class="pl-hint">Эти строки формы никуда не уедут, пока НСИ не заведёт правило.</span>
        <ul><li v-for="(u, i) in pub.unmapped" :key="'u' + i">{{ u }}</li></ul>
      </div>
    </div>

    <!-- ===== Табы: лист согласования / версии ===== -->
    <div v-if="card" class="cpp-tabs">
      <button class="tb" :class="{ active: tab === 'approvals' }" @click="tab = 'approvals'">
        Лист согласования <span class="tb-n">{{ approvals.length }}</span>
      </button>
      <button class="tb" :class="{ active: tab === 'versions' }" @click="tab = 'versions'">
        Версии <span class="tb-n">{{ versions.length }}</span>
      </button>
    </div>

    <div v-if="card && tab === 'approvals'" class="cpp-pane">
      <table v-if="approvals.length" class="data-table compact">
        <thead>
          <tr><th>Шаг</th><th>Кто</th><th>Решение</th><th>Комментарий</th><th class="col-num">Версия</th><th>Когда</th></tr>
        </thead>
        <tbody>
          <tr v-for="(a, i) in approvals" :key="i" :class="{ revoked: a.revoked }">
            <td>{{ stepTitle(a.step_code) }}</td>
            <td>{{ a.user_name || "—" }}</td>
            <td>
              <span class="badge" :class="decisionBadge(a.decision)">{{ decisionText(a) }}</span>
              <!-- ТЗ §2.3: решения не удаляются при возврате, а помечаются аннулированными. -->
              <span v-if="a.revoked" class="rv-note">аннулировано возвратом{{ a.revoked_reason ? `: ${a.revoked_reason}` : "" }}</span>
            </td>
            <td class="cm-cell">{{ a.comment || "—" }}</td>
            <td class="col-num">{{ a.version_no || "—" }}</td>
            <td class="dt-cell">{{ dt(a.decided_at) }}</td>
          </tr>
        </tbody>
      </table>
      <p v-else class="cpp-empty">Решений пока нет — карточка не выходила на согласование.</p>
    </div>

    <div v-if="card && tab === 'versions'" class="cpp-pane">
      <table v-if="versions.length" class="data-table compact">
        <thead>
          <tr><th class="col-num">№</th><th>Переход</th><th>Действие</th><th>Причина</th><th>Когда</th><th></th></tr>
        </thead>
        <tbody>
          <tr v-for="v in versions" :key="v.version_no">
            <td class="col-num">{{ v.version_no }}</td>
            <td>{{ v.step_from || "—" }} → {{ v.step_to || "—" }}</td>
            <td>{{ actionText(v.action) }}</td>
            <td class="cm-cell">{{ v.reason || "—" }}</td>
            <td class="dt-cell">{{ dt(v.created_at) }}</td>
            <td>
              <button class="btn btn-sm btn-ghost" :disabled="busy" @click="openVersion(v.version_no)">
                <Icon name="lucide:file-search" /> Снимок
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else class="cpp-empty">Версий нет: снимок пишется при каждом переходе по маршруту.</p>
    </div>

    <!-- ===== Модалка возврата ===== -->
    <PlansModal
      :open="ret.open"
      title="Вернуть карточку на доработку"
      subtitle="Возврат возможен только назад по маршруту, комментарий обязателен"
      @close="ret.open = false"
    >
      <label class="fld">
        <span class="fld-lbl">Вернуть на шаг <span class="req">*</span></span>
        <select v-model="ret.target" class="select">
          <option value="" disabled>— выберите шаг —</option>
          <option v-for="s in returnTargets" :key="s.step_code" :value="s.step_code">
            {{ s.step_code }} — {{ s.step_name || "шаг маршрута" }}
          </option>
        </select>
        <span v-if="!returnTargets.length" class="fld-hint">Раньше текущего шагов нет — возвращать некуда.</span>
      </label>
      <label class="fld">
        <span class="fld-lbl">Причина возврата <span class="req">*</span></span>
        <textarea v-model="ret.comment" class="textarea" rows="3" placeholder="что именно доработать"></textarea>
        <span class="fld-hint">Сервер отклоняет возврат без комментария (ТЗ §2.3). Решения от целевого шага и выше будут аннулированы.</span>
      </label>
      <template #footer>
        <button class="btn btn-ghost" @click="ret.open = false">Отмена</button>
        <button class="btn btn-danger" :disabled="!ret.target || !ret.comment.trim() || busy" @click="submitReturn">
          <Icon name="lucide:corner-up-left" /> Вернуть
        </button>
      </template>
    </PlansModal>

    <!-- ===== Модалка переоткрытия ===== -->
    <PlansModal
      :open="reopen.open"
      title="Переоткрыть утверждённый период"
      subtitle="Действие пишется в аудит: утверждённые суммы после этого могут измениться"
      @close="reopen.open = false"
    >
      <label class="fld">
        <span class="fld-lbl">Причина переоткрытия <span class="req">*</span></span>
        <textarea v-model="reopen.comment" class="textarea" rows="3" placeholder="решение финблока, номер обращения…"></textarea>
        <span class="fld-hint">Карточка вернётся на первый шаг маршрута, все решения будут аннулированы.</span>
      </label>
      <template #footer>
        <button class="btn btn-ghost" @click="reopen.open = false">Отмена</button>
        <button class="btn btn-danger" :disabled="!reopen.comment.trim() || busy" @click="submitReopen">
          <Icon name="lucide:unlock" /> Переоткрыть
        </button>
      </template>
    </PlansModal>

    <!-- ===== Модалка снимка версии ===== -->
    <PlansModal
      :open="ver.open"
      :title="`Снимок версии ${ver.no}`"
      subtitle="Значения, условия, курсы и ставки НДС на момент перехода"
      width="720px"
      @close="ver.open = false"
    >
      <pre class="ver-json">{{ ver.text }}</pre>
    </PlansModal>
  </section>
</template>

<script setup lang="ts">
import PlansModal from "~/components/plans/PlansModal.vue";
import {
  useMpConditions,
  type PlanCard,
  type RouteStep,
  type CardApproval,
  type CardVersion,
  type PublishResult
} from "~/composables/useMpConditions";
import { num } from "~/utils/format";

const props = withDefaults(
  defineProps<{
    cardId: number;
    /** Разрешена ли отправка на согласование: приходит из валидаций формы (can_submit). */
    canSubmit?: boolean;
    /** Чем именно заблокирована отправка — показываем рядом с кнопкой. */
    submitHint?: string;
  }>(),
  { canSubmit: true, submitHint: "" }
);
const emit = defineEmits<{ (e: "changed", card: PlanCard): void }>();

const api = useMpConditions();
const { hasRole, isAdmin } = useScope();
const isPlansAdmin = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));

const card = ref<PlanCard | null>(null);
const route = ref<RouteStep[]>([]);
const approvals = ref<CardApproval[]>([]);
const versions = ref<CardVersion[]>([]);
const pub = ref<PublishResult | null>(null);
const tab = ref<"approvals" | "versions">("approvals");
const loading = ref(true);
const busy = ref(false);
const error = ref("");
const note = ref("");
const ret = reactive({ open: false, target: "", comment: "" });
const reopen = reactive({ open: false, comment: "" });
const ver = reactive({ open: false, no: 0, text: "" });

// Порядок шагов — только включённые: выключенный «Финансист» не должен занимать
// место в цепочке и мешать выбору шага возврата (ТЗ §2.1 — маршрут это данные).
const enabledRoute = computed(() =>
  route.value.filter((s) => s.enabled).slice().sort((a, b) => a.sort_order - b.sort_order)
);
const curIdx = computed(() => enabledRoute.value.findIndex((s) => s.step_code === card.value?.step_code));
const currentStep = computed(() => (curIdx.value >= 0 ? enabledRoute.value[curIdx.value] : null));
const nextStep = computed(() =>
  curIdx.value >= 0 && curIdx.value + 1 < enabledRoute.value.length ? enabledRoute.value[curIdx.value + 1] : null
);
const isPassed = (code: string) => {
  const i = enabledRoute.value.findIndex((s) => s.step_code === code);
  return curIdx.value >= 0 && i >= 0 && i < curIdx.value;
};
const stepTitle = (code: string) => {
  const s = route.value.find((x) => x.step_code === code);
  return s ? `${s.step_code} — ${s.step_name || "шаг маршрута"}` : code || "—";
};

const CLOSED = ["approved", "published", "archived"];

// Человеческие метки: сервер отдаёт status_label технической строкой
// (on_approval_1.2), для человека собираем то же из статуса и шага.
const statusText = computed(() => {
  const c = card.value;
  if (!c) return "…";
  switch (c.status) {
    case "draft": return "черновик";
    case "on_approval": return `на согласовании (шаг ${c.step_code})`;
    case "returned": return `возвращено на ${c.step_code}`;
    case "approved": return "утверждено";
    case "published": return "опубликовано";
    case "publish_failed": return "ошибка публикации";
    case "archived": return "архив";
    default: return c.status_label || c.status;
  }
});
const statusBadge = computed(() => {
  switch (card.value?.status) {
    case "on_approval": return "badge-warn";
    case "returned": case "publish_failed": return "badge-neg";
    case "approved": case "published": return "badge-pos";
    case "archived": return "badge-dot";
    default: return "badge-info";
  }
});

const canSubmitStep = computed(() =>
  !!card.value && !CLOSED.includes(card.value.status) && currentStep.value?.kind === "fill"
);
const canApproveStep = computed(() =>
  !!card.value && !CLOSED.includes(card.value.status) && !!currentStep.value && currentStep.value.kind !== "fill"
);
const canReturn = computed(() =>
  !!card.value && !CLOSED.includes(card.value.status) && returnTargets.value.length > 0
);
const canReopen = computed(() =>
  isPlansAdmin.value && ["approved", "published", "publish_failed"].includes(card.value?.status || "")
);
const returnTargets = computed(() => (curIdx.value > 0 ? enabledRoute.value.slice(0, curIdx.value) : []));
const submitBlockedHint = computed(
  () => props.submitHint || "Есть блокирующие замечания — нажмите «Проверить» в форме и устраните их"
);

const pubStatus = computed(() => {
  switch (pub.value?.status) {
    case "ok": return "успешно";
    case "blocked": return "заблокировано";
    case "failed": return "ошибка";
    default: return pub.value?.status || "—";
  }
});
const pubBadge = computed(() => (pub.value?.status === "ok" ? "badge-pos" : pub.value?.status === "blocked" ? "badge-warn" : "badge-neg"));

const DECISIONS: Record<string, string> = {
  submit: "отправлено",
  approve: "согласовано",
  return: "возврат",
  reopen: "переоткрыто",
  archive: "в архив",
  publish: "публикация",
  auto_skipped: "автосогласование"
};
const decisionText = (a: CardApproval) =>
  a.decision === "return" && a.target_step ? `возврат на ${a.target_step}` : DECISIONS[a.decision] || a.decision;
const decisionBadge = (d: string) => {
  if (d === "approve") return "badge-pos";
  if (d === "return") return "badge-neg";
  if (d === "auto_skipped") return "badge-dot";
  return "badge-info";
};
const actionText = (a: string) => DECISIONS[a] || a || "—";

const dt = (s?: string) => {
  if (!s) return "—";
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString("ru-RU", { dateStyle: "short", timeStyle: "short" });
};

const errText = (e: unknown, fallback: string): string => {
  if (typeof e === "object" && e && "data" in e) {
    const d = (e as { data?: { error?: string } }).data;
    if (d?.error) return d.error;
  }
  return e instanceof Error ? e.message : fallback;
};

const load = async () => {
  loading.value = true;
  error.value = "";
  try {
    const v = await api.card(props.cardId);
    card.value = v.card;
    route.value = v.route || [];
    approvals.value = v.approvals || [];
    versions.value = v.versions || [];
  } catch (e: unknown) {
    error.value = errText(e, "Не удалось загрузить карточку процесса");
  } finally {
    loading.value = false;
  }
};

const ACTION_NOTE: Record<string, string> = {
  submit: "Форма отправлена на согласование.",
  approve: "Решение записано в лист согласования.",
  return: "Карточка возвращена; решения от целевого шага аннулированы.",
  reopen: "Период переоткрыт — запись снова разрешена."
};

const act = async (action: string, target = "", comment = "") => {
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    const c = await api.cardAction(props.cardId, { action, target_step: target, comment });
    card.value = c;
    emit("changed", c);
    note.value = ACTION_NOTE[action] || "Состояние карточки обновлено.";
    await load();
  } catch (e: unknown) {
    error.value = errText(e, "Переход не выполнен");
  } finally {
    busy.value = false;
  }
};

const openReturn = () => {
  ret.target = "";
  ret.comment = "";
  ret.open = true;
};
const submitReturn = async () => {
  if (!ret.target || !ret.comment.trim()) return;
  await act("return", ret.target, ret.comment.trim());
  if (!error.value) ret.open = false;
};
const submitReopen = async () => {
  if (!reopen.comment.trim()) return;
  await act("reopen", "", reopen.comment.trim());
  if (!error.value) {
    reopen.open = false;
    reopen.comment = "";
  }
};

const doPublish = async (mode: "dry_run" | "write") => {
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    pub.value = await api.publish(props.cardId, mode);
    if (mode === "write") await load();
  } catch (e: unknown) {
    error.value = errText(e, "Публикация не выполнена");
  } finally {
    busy.value = false;
  }
};

const openVersion = async (no: number) => {
  busy.value = true;
  try {
    const payload = await api.versionPayload(props.cardId, no);
    ver.no = no;
    ver.text = JSON.stringify(payload, null, 2);
    ver.open = true;
  } catch (e: unknown) {
    error.value = errText(e, "Снимок версии недоступен");
  } finally {
    busy.value = false;
  }
};

watch(() => props.cardId, load);
onMounted(load);
defineExpose({ reload: load });
</script>

<style scoped>
.cpp {
  display: flex;
  flex-direction: column;
  gap: var(--sp-4);
  padding: var(--sp-5);
}
.cpp-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--sp-4);
  flex-wrap: wrap;
}
.cpp-id { display: flex; flex-direction: column; gap: 2px; }
.cpp-title { font-size: var(--fs-md); font-weight: var(--fw-semibold); color: var(--text-strong); }
.cpp-meta { display: flex; gap: var(--sp-2); flex-wrap: wrap; font-size: var(--fs-2xs); color: var(--text-muted); }
.cpp-form { font-family: var(--font-mono); }
.cpp-state { display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.cpp-lock { display: inline-flex; align-items: center; gap: 3px; font-size: var(--fs-2xs); color: var(--neg); }
.cpp-mode { font-size: var(--fs-2xs); color: var(--text-muted); }
.cpp-mode.inv { color: var(--accent); }

.cpp-banner { padding: var(--sp-3) var(--sp-4); border-radius: var(--rd-4); font-size: var(--fs-sm); margin: 0; }
.cpp-banner.neg { background: var(--neg-soft); color: var(--neg-strong); }
.cpp-banner.pos { background: var(--pos-soft); color: var(--pos-strong); }
.cpp-loading { display: flex; align-items: center; gap: 6px; font-size: var(--fs-sm); color: var(--text-secondary); margin: 0; }

.cpp-flow {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  flex-wrap: wrap;
  background: var(--bg-surface-2);
  border-radius: var(--rd-4);
  padding: var(--sp-3) var(--sp-4);
}
.fl-step { display: flex; flex-direction: column; gap: 1px; min-width: 160px; }
.fl-lbl { font-size: var(--fs-2xs); text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); }
.fl-val { font-size: var(--fs-sm); color: var(--text-strong); }
.fl-sub { font-size: var(--fs-2xs); color: var(--text-secondary); }
.fl-arrow { color: var(--text-muted); }
.fl-route { display: flex; gap: 4px; margin-left: auto; }
.fl-chip {
  font-family: var(--font-mono);
  font-size: var(--fs-2xs);
  padding: 2px 8px;
  border: 1px solid var(--border);
  border-radius: var(--rd-pill, 999px);
  color: var(--text-muted);
}
.fl-chip.done { border-color: var(--pos); color: var(--pos-strong); }
.fl-chip.on { background: var(--accent-soft); border-color: var(--accent); color: var(--accent); }

.cpp-actions { display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.cpp-sep { flex: 1; }
.cpp-hint { font-size: var(--fs-2xs); color: var(--warn); }

.cpp-pub {
  border: 1px solid var(--border);
  border-radius: var(--rd-4);
  padding: var(--sp-4);
  display: flex;
  flex-direction: column;
  gap: var(--sp-3);
}
.pub-head { display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.pub-title { font-size: var(--fs-sm); font-weight: var(--fw-semibold); color: var(--text-strong); }
.pub-target { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); }
.pub-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: var(--sp-3); }
.pub-cell { display: flex; flex-direction: column; gap: 1px; }
.pub-lbl { font-size: var(--fs-2xs); color: var(--text-secondary); }
.pub-val { font-family: var(--font-mono); font-variant-numeric: tabular-nums; font-size: var(--fs-md); }
.pub-val.ok { color: var(--pos-strong); }
.pub-val.bad { color: var(--neg-strong); }
.pub-list { border-radius: var(--rd-4); padding: var(--sp-3) var(--sp-4); display: flex; flex-direction: column; gap: 2px; }
.pub-list.warn { background: var(--warn-soft); color: var(--warn); }
.pub-list.neg { background: var(--neg-soft); color: var(--neg-strong); }
.pl-title { display: inline-flex; align-items: center; gap: 4px; font-size: var(--fs-sm); font-weight: var(--fw-semibold); }
.pl-hint { font-size: var(--fs-2xs); opacity: 0.85; }
.pub-list ul { margin: var(--sp-2) 0 0; padding-left: var(--sp-6); font-size: var(--fs-2xs); font-family: var(--font-mono); }

.cpp-tabs { display: flex; gap: var(--sp-2); border-bottom: 1px solid var(--border); }
.tb {
  border: 0;
  background: none;
  padding: var(--sp-2) var(--sp-4);
  font-size: var(--fs-sm);
  color: var(--text-secondary);
  cursor: pointer;
  border-bottom: 2px solid transparent;
}
.tb.active { color: var(--text-strong); border-bottom-color: var(--accent); }
.tb-n { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); }
.cpp-pane { overflow-x: auto; }
.cpp-empty { font-size: var(--fs-sm); color: var(--text-muted); padding: var(--sp-4); margin: 0; }
.cm-cell { max-width: 320px; font-size: var(--fs-sm); }
.dt-cell { font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-secondary); white-space: nowrap; }
.revoked td { text-decoration: line-through; color: var(--text-muted); }
.rv-note { display: block; font-size: var(--fs-2xs); color: var(--neg); text-decoration: none; }

.fld { display: block; margin-bottom: var(--sp-5); }
.fld-lbl { display: block; font-size: var(--fs-sm); color: var(--text-secondary); margin-bottom: var(--sp-2); }
.fld-hint { display: block; margin-top: var(--sp-2); font-size: var(--fs-xs); color: var(--text-muted); }
.req { color: var(--neg); }
.ver-json {
  max-height: 60vh;
  overflow: auto;
  font-family: var(--font-mono);
  font-size: var(--fs-2xs);
  background: var(--bg-surface-2);
  border-radius: var(--rd-3);
  padding: var(--sp-3);
  margin: 0;
}
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
