<template>
  <!-- Блок не рендерится вовсе, если разбор не настроен на сервере или у
       пользователя нет права: пустая кнопка, которая всегда отвечает ошибкой,
       хуже отсутствующей. -->
  <section v-if="available" class="insight">
    <div class="insight-head">
      <span class="insight-badge">Разбор ИИ</span>
      <span class="insight-title">{{ blockTitle }}</span>
      <span v-if="result?.cached" class="insight-meta" :title="cachedTitle">из кэша</span>
      <button class="insight-btn" :disabled="loading || !hasData" @click="run(false)">
        {{ loading ? 'Разбираю…' : (result ? 'Обновить' : 'Разобрать блок') }}
      </button>
      <button v-if="result && !loading" class="insight-btn ghost" title="Пересчитать, игнорируя кэш"
              @click="run(true)">Пересчитать</button>
    </div>

    <p v-if="!hasData" class="insight-note">
      Нет данных для разбора — сначала загрузите период.
    </p>

    <p v-if="loading" class="insight-note">
      Считаю факты и запрашиваю наблюдения. Обычно 10–40 секунд.
    </p>

    <p v-if="error" class="insight-error">{{ error }}</p>

    <template v-if="result">
      <!-- Модель отвалилась — показываем посчитанные факты. Блок остаётся
           полезным даже без LLM: числа-то настоящие. -->
      <p v-if="result.degraded" class="insight-error">
        Наблюдения недоступны: {{ result.error }}. Ниже — факты, посчитанные по графикам.
      </p>

      <ul v-if="result.observations?.length" class="insight-list">
        <li v-for="(obs, i) in result.observations" :key="i" :class="['obs', obs.kind]">
          <span class="obs-mark" aria-hidden="true"></span>
          <span class="obs-text">
            {{ obs.text }}
            <span v-if="obs.unverified_numbers?.length" class="obs-warn"
                  :title="'Эти числа модель посчитала сама, в данных графиков их нет: ' + obs.unverified_numbers.join(', ')">
              расчёт модели
            </span>
          </span>
        </li>
      </ul>

      <details v-if="result.facts?.length" class="insight-facts">
        <summary>Посчитанные факты ({{ result.facts.length }})</summary>
        <ul>
          <li v-for="(f, i) in result.facts" :key="i">{{ f }}</li>
        </ul>
      </details>

      <p class="insight-foot">
        Наблюдения, а не выводы — проверяйте перед использованием в отчётах.
        Причины по этим данным не определяются.
        <span v-if="result.model" class="insight-meta">· {{ result.model }}</span>
        <span v-if="result.elapsed_ms" class="insight-meta">· {{ (result.elapsed_ms / 1000).toFixed(1) }} с</span>
        <span v-if="result.usage?.cost" class="insight-meta">· {{ result.usage.cost }} $</span>
      </p>
    </template>

    <!-- Углублённое исследование: обзор отвечает «что», это — «почему».
         Агент сам ходит за данными инструментами раздела, поэтому здесь можно
         спрашивать про причины, а не только про факты. -->
    <div class="ask">
      <!-- Переписка: уточняющий вопрос опирается на прошлые раунды, поэтому
           показываем их подряд, а не заменяем ответ новым. -->
      <div v-for="(round, ri) in rounds" :key="ri" class="round">
        <p class="round-q">{{ round.question }}</p>

        <p v-if="round.degraded" class="insight-error">
          Исследование не завершилось: {{ round.error }}
        </p>
        <!-- Ответ собран после сбоя по неполным данным — это надо видеть, иначе
             он читается как полноценный результат исследования. -->
        <p v-else-if="round.rescued" class="ask-rescued">
          Связь с моделью прервалась на середине — вывод сделан по тем данным,
          которые агент успел собрать. Стоит перепроверить.
        </p>
        <p v-if="round.answer" class="ask-answer">{{ round.answer }}</p>

        <ul v-if="round.findings?.length" class="insight-list">
          <li v-for="(f, i) in round.findings" :key="i" :class="['obs', f.kind]">
            <span class="obs-mark" aria-hidden="true"></span>
            <span class="obs-text">
              {{ f.text }}
              <span v-if="f.evidence" class="obs-evidence">{{ f.evidence }}</span>
            </span>
          </li>
        </ul>

        <p v-if="round.data_quality" class="ask-quality">
          <strong>Качество данных:</strong> {{ round.data_quality }}
        </p>

        <template v-if="round.next_steps?.length">
          <p class="ask-subhead">Что проверить дальше</p>
          <ul class="ask-steps">
            <li v-for="(n, i) in round.next_steps" :key="i">{{ n }}</li>
          </ul>
        </template>

        <details v-if="round.trace?.length" class="insight-facts">
          <summary>
            Как получен ответ: {{ round.trace.length }}
            {{ round.trace.length === 1 ? 'запрос' : 'запроса' }} к данным
          </summary>
          <ul>
            <li v-for="(t, i) in round.trace" :key="i">
              {{ t.step }}. {{ t.tool }}({{ shortArgs(t.args) }}){{ t.ok ? '' : ' — ошибка' }}
            </li>
          </ul>
        </details>

        <p class="insight-foot">
          <span v-if="round.cached" class="insight-meta">из кэша · </span>
          <span v-if="round.model" class="insight-meta">{{ round.model }}</span>
          <span v-if="round.elapsed_ms" class="insight-meta"> · {{ (round.elapsed_ms / 1000).toFixed(0) }} с</span>
          <span v-if="round.usage?.cost" class="insight-meta"> · {{ round.usage.cost }} $</span>
          <span v-if="round.confidence" class="insight-meta"> · уверенность: {{ confidenceLabel(round.confidence) }}</span>
        </p>
      </div>

      <form class="ask-form" @submit.prevent="ask(false)">
        <input v-model="question" class="ask-input" type="text"
               :disabled="asking"
               :placeholder="rounds.length
                 ? 'Уточнить: а где именно, с чем связано, а в прошлом году…'
                 : 'Спросить по этому срезу: почему, где именно, что изменилось…'" />
        <button class="insight-btn" type="submit" :disabled="asking || !question.trim()">
          {{ asking ? 'Исследую…' : (rounds.length ? 'Уточнить' : 'Разобраться') }}
        </button>
        <button v-if="rounds.length && !asking" class="insight-btn ghost" type="button"
                title="Начать разговор с чистого листа — прошлые ответы не будут учитываться"
                @click="resetThread">Новая тема</button>
      </form>

      <p v-if="asking" class="insight-note">
        <template v-if="progressLabel">Агент {{ progressLabel }}…</template>
        <template v-else>Агент планирует исследование…</template>
        <span class="insight-meta">· обычно 20–60 секунд, можно продолжать работу</span>
      </p>
      <p v-if="askError" class="insight-error">{{ askError }}</p>

    </div>
  </section>
</template>

<script setup lang="ts">
/**
 * Блок «Разбор ИИ» под ЛОГИЧЕСКОЙ СЕКЦИЕЙ графиков (не под каждой карточкой).
 *
 * Секция целиком даёт модели возможность связать графики между собой — «маржа
 * выросла, а маржинальность просела» по одному графику не увидеть. Поэтому
 * компонент принимает массив графиков секции, а не один набор данных.
 *
 * Наверх (на бэкенд) уходят те же серии, что нарисованы на карточках, — их этот
 * же пользователь только что легально получил из /margin или /commercial.
 * Считает факты бэкенд (app/insights.py), модель лишь формулирует текст: числа
 * в наблюдениях приходят из Python, а несошедшиеся помечаются «расчёт модели».
 */
import { computed, ref } from "vue";

const props = defineProps<{
  /** Машинный ключ секции для журнала: 'margin_output', 'norm_vs_fact', … */
  block: string;
  /** Заголовок секции, как он показан пользователю. */
  blockTitle: string;
  /** Графики секции: [{ title, note?, series: { labels, datasets } }] */
  charts: Array<{ title: string; note?: string; series: any }>;
  /** Период, валюта, фильтры — что человек видел на экране. */
  context?: Record<string, any>;
}>();

const config = useRuntimeConfig();
const apiBase = computed(() =>
  config.public.costOnly ? "" : ((config.public.apiBase as string) || "")
);

const { user } = useAuth();
const { can } = useCostPermission();

/** Пользователь в заголовке обязателен: глобального перехватчика $fetch в
 * проекте нет, каждая страница раздела шлёт X-Cost-User сама. */
const fetchHeaders = computed(() => {
  const h: Record<string, string> = {};
  if (user.value?.email) h["X-Cost-User"] = user.value.email;
  return h;
});

/** Настроен ли разбор на сервере. Состояние общее для всех блоков страницы:
 * иначе три секции дадут три одинаковых запроса статуса. */
const status = useState<{ enabled: boolean; model: string } | null>(
  "cost-insights-status", () => null
);
const statusInFlight = useState<Promise<void> | null>(
  "cost-insights-status-inflight", () => null
);

async function loadStatus(): Promise<void> {
  if (status.value || statusInFlight.value) return statusInFlight.value ?? undefined;
  const p = (async () => {
    try {
      status.value = await $fetch<any>(`${apiBase.value}/api/cost/insights/status`,
                                       { headers: fetchHeaders.value });
    } catch {
      // Недоступный статус трактуем как «выключено»: блок просто не появится.
      status.value = { enabled: false, model: "" };
    }
  })().finally(() => { statusInFlight.value = null; });
  statusInFlight.value = p;
  return p;
}

onMounted(loadStatus);

const available = computed(() => !!status.value?.enabled && can("cost:insights"));

const hasData = computed(() =>
  (props.charts || []).some((c) => (c?.series?.datasets || []).some(
    (ds: any) => (ds?.data || []).length > 0))
);

const loading = ref(false);
const error = ref("");
const result = ref<any>(null);

const cachedTitle = computed(() =>
  result.value?.cached_at ? `Разобрано ${new Date(result.value.cached_at).toLocaleString("ru")}` : ""
);

// ── Углублённое исследование ────────────────────────────────────────────────

const question = ref("");
const asking = ref(false);
const askError = ref("");

/** Ветка диалога на сервере. Пока null — следующий вопрос откроет новую. */
const threadId = ref<string | null>(null);
/** Завершённые раунды переписки, в порядке заданных вопросов. */
const rounds = ref<any[]>([]);

const CONFIDENCE: Record<string, string> = {
  high: "высокая", medium: "средняя", low: "низкая",
};
function confidenceLabel(value: string): string {
  return CONFIDENCE[value] || value || "";
}

/** Начать разговор заново: прошлые раунды больше не пойдут в контекст. */
function resetThread(): void {
  stopPolling();
  threadId.value = null;
  rounds.value = [];
  askError.value = "";
  question.value = "";
}

/** Аргументы шага в одну строку — в трассе важно «что спрашивали», не JSON. */
function shortArgs(args: any): string {
  if (!args || typeof args !== "object") return "";
  const parts: string[] = [];
  for (const [key, value] of Object.entries(args)) {
    if (value === null || value === undefined || value === "") continue;
    const text = typeof value === "object"
      ? Object.entries(value as Record<string, any>)
          .map(([k, v]) => `${k}=${Array.isArray(v) ? v.join("|") : v}`).join(", ")
      : String(value);
    if (text) parts.push(key === "filters" ? text : `${key}: ${text}`);
  }
  return parts.join("; ");
}

/** Что агент делает прямо сейчас — из прогресса задачи. */
const progress = ref<{ step?: number; tool?: string; sql_calls?: number } | null>(null);

const TOOL_LABELS: Record<string, string> = {
  metrics: "смотрит показатели среза",
  breakdown: "сравнивает товарные группы",
  drill: "проваливается внутрь группы",
  articles: "разбирает артикулы",
  fact_vs_norm: "сверяет норматив с фактом",
};

const progressLabel = computed(() => {
  const p = progress.value;
  if (!p?.tool) return "";
  const what = TOOL_LABELS[p.tool] || p.tool;
  return `шаг ${p.step}: ${what}`;
});

/** Опрос статуса. Исследование живёт фоновой задачей на сервере, поэтому
 * HTTP-запросы здесь короткие и таймаут прокси на них не влияет — из-за него
 * синхронный вариант и обрывался на 60-й секунде. */
const POLL_INTERVAL_MS = 2000;
const POLL_LIMIT_MS = 300000;

let pollTimer: ReturnType<typeof setTimeout> | null = null;

function stopPolling(): void {
  if (pollTimer !== null) {
    clearTimeout(pollTimer);
    pollTimer = null;
  }
}

// Уходя со страницы, опрос надо погасить: иначе он продолжает дёргать сервер
// из размонтированного компонента.
onBeforeUnmount(stopPolling);

async function poll(jobId: string, startedAt: number, asked: string): Promise<void> {
  try {
    const job: any = await $fetch(
      `${apiBase.value}/api/cost/insights/ask/${jobId}`,
      { headers: fetchHeaders.value }
    );

    if (job.status === "running") {
      progress.value = job.progress || null;
      if (Date.now() - startedAt > POLL_LIMIT_MS) {
        askError.value = "Исследование идёт слишком долго — прервано на стороне интерфейса";
        asking.value = false;
        return;
      }
      pollTimer = setTimeout(() => poll(jobId, startedAt, asked), POLL_INTERVAL_MS);
      return;
    }

    // Вопрос подставляем свой: в ответе сервера он есть, но в переписке важно
    // показать ровно то, что напечатал человек.
    finishRound({ ...job, question: job.question || asked });
  } catch (e: any) {
    askError.value = e?.data?.detail || e?.message || "Не удалось получить результат";
    asking.value = false;
  }
}

function finishRound(job: any): void {
  rounds.value = [...rounds.value, job];
  if (job.thread_id) threadId.value = job.thread_id;
  progress.value = null;
  asking.value = false;
  question.value = "";
}

async function ask(force: boolean): Promise<void> {
  const text = question.value.trim();
  if (!text || asking.value) return;
  stopPolling();
  asking.value = true;
  askError.value = "";
  progress.value = null;
  try {
    const started: any = await $fetch(`${apiBase.value}/api/cost/insights/ask`, {
      method: "POST",
      headers: fetchHeaders.value,
      body: {
        question: text,
        context: props.context || {},
        force,
        // Пусто в первом вопросе — сервер откроет ветку и вернёт её id.
        thread_id: threadId.value,
      },
    });

    // Ответ из кэша приходит готовым — опрашивать нечего.
    if (started.status === "done" || started.status === "failed") {
      finishRound({ ...started, question: text });
      return;
    }
    if (!started.job_id) {
      askError.value = "Сервер не вернул идентификатор исследования";
      asking.value = false;
      return;
    }
    if (started.thread_id) threadId.value = started.thread_id;
    await poll(started.job_id, Date.now(), text);
  } catch (e: any) {
    askError.value = e?.data?.detail || e?.message || "Не удалось запустить исследование";
    asking.value = false;
  }
}

async function run(force: boolean): Promise<void> {
  if (loading.value || !hasData.value) return;
  loading.value = true;
  error.value = "";
  try {
    result.value = await $fetch<any>(`${apiBase.value}/api/cost/insights/block`, {
      method: "POST",
      headers: fetchHeaders.value,
      body: {
        block: props.block,
        block_title: props.blockTitle,
        context: props.context || {},
        charts: props.charts,
        force,
      },
    });
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || "Не удалось получить разбор";
  } finally {
    loading.value = false;
  }
}
</script>

<style scoped>
/* Визуально блок отличается от карточек с данными: тонкая рамка слева и
 * приглушённый фон, чтобы текст модели не читался как сами данные. */
.insight {
  margin: var(--sp-3) 0 var(--sp-6);
  padding: var(--sp-4);
  background: var(--bg-surface-2);
  border: 1px solid var(--border);
  border-left: 2px solid var(--accent);
  border-radius: var(--rd-3);
}

.insight-head {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  flex-wrap: wrap;
}

.insight-badge {
  font-size: var(--fs-2xs);
  font-weight: var(--fw-semibold);
  letter-spacing: .04em;
  text-transform: uppercase;
  color: var(--accent);
}

.insight-title {
  font-size: var(--fs-sm);
  color: var(--text-secondary);
  margin-right: auto;
}

.insight-btn {
  padding: var(--sp-1) var(--sp-3);
  font-size: var(--fs-xs);
  font-family: inherit;
  color: var(--accent-fg);
  background: var(--accent);
  border: 1px solid var(--accent);
  border-radius: var(--rd-2);
  cursor: pointer;
  transition: background var(--t-fast);
}
.insight-btn:hover:not(:disabled) { background: var(--accent-hover); }
.insight-btn:disabled { opacity: .5; cursor: default; }

.insight-btn.ghost {
  color: var(--text-secondary);
  background: transparent;
  border-color: var(--border);
}
.insight-btn.ghost:hover:not(:disabled) { background: var(--bg-surface-3); }

.insight-note, .insight-error, .insight-foot {
  margin: var(--sp-3) 0 0;
  font-size: var(--fs-xs);
  line-height: var(--lh-relaxed);
  color: var(--text-muted);
}
.insight-error { color: var(--neg); }

.insight-list {
  margin: var(--sp-3) 0 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: var(--sp-2);
}

.obs {
  display: flex;
  gap: var(--sp-2);
  font-size: var(--fs-sm);
  line-height: var(--lh-relaxed);
  color: var(--text-primary);
}

/* Метка вида наблюдения. Цветом, а не иконкой-эмодзи: в разделе всё оформление
 * идёт токенами дизайн-системы. */
.obs-mark {
  flex: 0 0 auto;
  width: 3px;
  margin-top: .35em;
  height: 1em;
  border-radius: var(--rd-pill);
  background: var(--text-muted);
}
.obs.attention .obs-mark { background: var(--warn); }
.obs.risk .obs-mark { background: var(--neg); }

.obs-warn {
  margin-left: var(--sp-1);
  padding: 0 var(--sp-1);
  font-size: var(--fs-2xs);
  color: var(--warn);
  border: 1px dashed var(--warn);
  border-radius: var(--rd-2);
  cursor: help;
  white-space: nowrap;
}

.insight-facts {
  margin-top: var(--sp-3);
  font-size: var(--fs-xs);
  color: var(--text-secondary);
}
.insight-facts summary { cursor: pointer; color: var(--text-muted); }
.insight-facts ul {
  margin: var(--sp-2) 0 0;
  padding-left: var(--sp-4);
  display: grid;
  gap: 2px;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
}

.insight-meta {
  font-size: var(--fs-2xs);
  color: var(--text-muted);
  white-space: nowrap;
}

/* Блок исследования отделён от обзора линией: это другой источник — не пересказ
 * графиков, а запросы к данным по шагам. */
.ask {
  margin-top: var(--sp-4);
  padding-top: var(--sp-3);
  border-top: 1px solid var(--border);
}

.ask-form {
  display: flex;
  gap: var(--sp-2);
}

.ask-input {
  flex: 1;
  min-width: 0;
  padding: var(--sp-1) var(--sp-2);
  font-family: inherit;
  font-size: var(--fs-sm);
  color: var(--text-primary);
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
}
.ask-input:focus {
  outline: none;
  border-color: var(--border-focus);
  box-shadow: var(--shadow-focus);
}
.ask-input:disabled { opacity: .6; }

.ask-answer {
  margin: var(--sp-3) 0 0;
  font-size: var(--fs-sm);
  line-height: var(--lh-relaxed);
  color: var(--text-strong);
}

.ask-quality {
  margin: var(--sp-3) 0 0;
  font-size: var(--fs-xs);
  line-height: var(--lh-relaxed);
  color: var(--text-secondary);
}

/* Предупреждение о неполном исследовании: цветом warn, а не neg — ответ есть и
 * он по настоящим данным, просто собран не до конца. */
.ask-rescued {
  margin: 0 0 var(--sp-2);
  padding: var(--sp-1) var(--sp-2);
  font-size: var(--fs-xs);
  line-height: var(--lh-relaxed);
  color: var(--warn);
  background: var(--warn-soft);
  border-radius: var(--rd-2);
}

.ask-subhead {
  margin: var(--sp-3) 0 var(--sp-1);
  font-size: var(--fs-2xs);
  font-weight: var(--fw-semibold);
  letter-spacing: .04em;
  text-transform: uppercase;
  color: var(--text-muted);
}

.ask-steps {
  margin: 0;
  padding-left: var(--sp-4);
  display: grid;
  gap: 2px;
  font-size: var(--fs-xs);
  line-height: var(--lh-relaxed);
  color: var(--text-secondary);
}

/* Раунд переписки. Вопрос выделен, чтобы в длинной ветке было видно, где
 * заканчивается один ответ и начинается следующий. */
.round {
  margin-bottom: var(--sp-4);
  padding-bottom: var(--sp-3);
  border-bottom: 1px dashed var(--border);
}
.round:last-of-type { border-bottom: none; }

.round-q {
  margin: 0 0 var(--sp-2);
  font-size: var(--fs-sm);
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
}
.round-q::before {
  content: "— ";
  color: var(--accent);
}

/* Чем подтверждено наблюдение — отдельной строкой моноширинным: это ссылка на
 * шаг исследования, а не часть текста вывода. */
.obs-evidence {
  display: block;
  margin-top: 2px;
  font-family: var(--font-mono);
  font-size: var(--fs-2xs);
  color: var(--text-muted);
}

.ask-again { margin-left: var(--sp-2); }
</style>
