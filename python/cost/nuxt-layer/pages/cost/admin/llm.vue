<template>
  <div class="page">
    <h1 class="title">Подключение к языковой модели</h1>

    <p v-if="permLoading" class="note">Проверяю права…</p>

    <div v-else-if="!can('cost:llm_admin')" class="cost-error">
      Нужно право <code>cost:llm_admin</code>. Обратитесь к администратору раздела.
    </div>

    <template v-else>
      <p class="note">
        Настройки применяются к «Разбору ИИ» и углублённым исследованиям без
        перезапуска сервиса. Заданное здесь перекрывает переменные
        <code>COST_LLM_*</code> из окружения; пустое поле — значит «брать из
        окружения».
      </p>

      <p v-if="error" class="cost-error">{{ error }}</p>
      <p v-if="saved" class="ok">Сохранено{{ savedBy }}.</p>

      <form class="card form" @submit.prevent="save">
        <label class="row switch">
          <input v-model="form.enabled" type="checkbox" />
          <span>Разбор включён<span v-if="!view.api_key_set" class="hint">
            — сначала задайте ключ, иначе останется выключенным</span></span>
        </label>

        <label class="row">
          <span class="lbl">Адрес провайдера
            <span class="src">{{ view.sources?.api_base }}</span></span>
          <input v-model.trim="form.api_base" class="inp" type="text"
                 placeholder="https://opencode.ai/zen/v1" />
          <span class="hint">
            OpenAI-совместимый эндпоинт, без <code>/chat/completions</code> на конце.
            <template v-if="view.allowed_hosts?.length">
              Разрешённые хосты: {{ view.allowed_hosts.join(', ') }}.
            </template>
            <template v-else-if="!view.allow_private">
              Только https и публичный адрес: запросы во внутреннюю сеть запрещены.
            </template>
            <template v-else>Приватные адреса разрешены (dev).</template>
          </span>
        </label>

        <label class="row">
          <span class="lbl">Ключ
            <span class="src">{{ view.sources?.api_key }}</span></span>
          <input v-model.trim="form.api_key" class="inp" type="password"
                 autocomplete="off"
                 :placeholder="view.api_key_mask || 'не задан'" />
          <span class="hint">
            Показывается маской и никогда не отдаётся наружу целиком. Пустое
            поле — оставить прежний ключ.
          </span>
        </label>

        <label class="row">
          <span class="lbl">Модель
            <span class="src">{{ view.sources?.model }}</span></span>
          <span class="model-row">
            <select v-if="models.length" v-model="form.model" class="inp">
              <option v-for="m in models" :key="m" :value="m">{{ m }}</option>
            </select>
            <input v-else v-model.trim="form.model" class="inp" type="text"
                   placeholder="deepseek-v4-flash" />
            <button type="button" class="btn ghost" :disabled="loadingModels"
                    @click="loadModels">
              {{ loadingModels ? 'Запрашиваю…' : 'Список от провайдера' }}
            </button>
          </span>
          <span v-if="modelsError" class="hint warn">{{ modelsError }}</span>
        </label>

        <label class="row">
          <span class="lbl">Размышления модели
            <span class="src">{{ view.sources?.reasoning }}</span></span>
          <select v-model="form.reasoning" class="inp narrow">
            <option value="none">none — выключены (рекомендуется)</option>
            <option value="low">low</option>
            <option value="medium">medium</option>
            <option value="high">high</option>
          </select>
          <span class="hint">
            Замер 02.09.2026: с выключенными размышлениями ответ 2–3 с, с
            включёнными 17–127 с и один таймаут из пяти — лимит токенов их не
            ограничивает. Включаете — поднимите таймаут минимум до 180 с.
          </span>
        </label>

        <div class="pair">
          <label class="row">
            <span class="lbl">Таймаут одного вызова, с</span>
            <input v-model.number="form.timeout_s" class="inp narrow" type="number"
                   min="5" max="600" />
          </label>
          <label class="row">
            <span class="lbl">Бюджет исследования, с</span>
            <input v-model.number="form.agent_budget_s" class="inp narrow" type="number"
                   min="20" max="900" />
            <span class="hint">Сколько времени агент может копать один вопрос.</span>
          </label>
        </div>

        <div class="actions">
          <button class="btn" type="submit" :disabled="saving">
            {{ saving ? 'Сохраняю…' : 'Сохранить' }}
          </button>
          <button class="btn ghost" type="button" :disabled="testing" @click="test">
            {{ testing ? 'Проверяю…' : 'Проверить подключение' }}
          </button>
        </div>

        <!-- Проверка до того, как настройку увидят пользователи: иначе
             неработающая модель обнаруживается на живых вопросах. -->
        <p v-if="testResult" :class="['test', testResult.ok ? 'ok' : 'cost-error']">
          <template v-if="testResult.ok">
            Работает: {{ testResult.model }}, {{ (testResult.elapsed_ms / 1000).toFixed(1) }} с,
            ответ «{{ testResult.answer }}»
            <span v-if="testResult.cost">· {{ testResult.cost }} $</span>
          </template>
          <template v-else>
            Не работает: {{ testResult.error }}
            <span v-if="testResult.status">(HTTP {{ testResult.status }})</span>
          </template>
        </p>
      </form>

      <section v-if="usage" class="card">
        <h2 class="subtitle">Расход за {{ usage.days }} дней</h2>
        <table class="usage">
          <thead>
            <tr><th></th><th>вызовов</th><th>сбоев</th><th>токенов</th><th>стоимость</th></tr>
          </thead>
          <tbody>
            <tr>
              <td>Разбор блоков</td>
              <td>{{ usage.blocks.runs }}</td>
              <td :class="{ warn: usage.blocks.failed }">{{ usage.blocks.failed }}</td>
              <td>{{ fmtTokens(usage.blocks) }}</td>
              <td>{{ usage.blocks.cost }} $</td>
            </tr>
            <tr>
              <td>Исследования</td>
              <td>{{ usage.asks.runs }}</td>
              <td :class="{ warn: usage.asks.failed }">{{ usage.asks.failed }}</td>
              <td>{{ fmtTokens(usage.asks) }}</td>
              <td>{{ usage.asks.cost }} $</td>
            </tr>
          </tbody>
          <tfoot>
            <tr>
              <td>Итого</td><td></td><td></td><td></td>
              <td><strong>{{ usage.cost_total }} $</strong></td>
            </tr>
          </tfoot>
        </table>
        <p v-if="usage.asks.avg_ms" class="note">
          Среднее исследование — {{ (usage.asks.avg_ms / 1000).toFixed(0) }} с.
        </p>
      </section>

      <p v-if="view.updated_at" class="note">
        Последнее изменение: {{ new Date(view.updated_at).toLocaleString('ru') }}
        <template v-if="view.updated_by">, {{ view.updated_by }}</template>.
      </p>
    </template>
  </div>
</template>

<script setup lang="ts">
/**
 * Настройка подключения к языковой модели.
 *
 * Зачем страница. На проде `.env` приходит из CI-переменной через docker
 * config, и смена модели там стоила бы полного деплоя стека. А переключаться
 * приходится по ходу дня: у провайдера могут разом отвалиться целые семейства
 * моделей (02.09.2026 у opencode zen так ушли Claude, GPT и Gemini), да и
 * «думающие» модели непригодны без отключения размышлений.
 */
import { computed, reactive, ref } from "vue";
import { useCostPermission } from "~/composables/useCostPermission";

const config = useRuntimeConfig();
const apiBase = computed(() =>
  config.public.costOnly ? "" : ((config.public.apiBase as string) || "")
);

const { user } = useAuth();
const { can, loading: permLoading } = useCostPermission();

const fetchHeaders = computed(() => {
  const h: Record<string, string> = {};
  if (user.value?.email) h["X-Cost-User"] = user.value.email;
  return h;
});

const view = ref<any>({});
const form = reactive<any>({
  enabled: false, api_base: "", api_key: "", model: "",
  reasoning: "none", timeout_s: 60, agent_budget_s: 120,
});

const error = ref("");
const saved = ref(false);
const saving = ref(false);
const testing = ref(false);
const testResult = ref<any>(null);
const models = ref<string[]>([]);
const loadingModels = ref(false);
const modelsError = ref("");
const usage = ref<any>(null);

const savedBy = computed(() => (view.value.updated_by ? `, ${view.value.updated_by}` : ""));

function fmtTokens(row: any): string {
  const p = row.prompt_tokens || 0;
  const c = row.completion_tokens || 0;
  return `${(p + c).toLocaleString("ru")}`;
}

/** Форму заполняем ЭФФЕКТИВНЫМИ значениями (с учётом окружения): иначе поля
 * выглядят пустыми, хотя разбор работает, и первое же сохранение затёрло бы
 * настройку из .env пустотой. */
function applyView(data: any): void {
  view.value = data || {};
  form.enabled = !!data?.enabled;
  form.api_base = data?.api_base || "";
  form.model = data?.model || "";
  form.reasoning = data?.reasoning || "none";
  form.timeout_s = data?.timeout_s || 60;
  form.agent_budget_s = data?.agent_budget_s || 120;
  form.api_key = "";
}

async function load(): Promise<void> {
  error.value = "";
  try {
    applyView(await $fetch<any>(`${apiBase.value}/api/cost/admin/llm`,
                                { headers: fetchHeaders.value }));
    usage.value = await $fetch<any>(`${apiBase.value}/api/cost/admin/llm/usage`,
                                    { headers: fetchHeaders.value });
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || "Не удалось загрузить настройки";
  }
}

async function save(): Promise<void> {
  saving.value = true;
  error.value = "";
  saved.value = false;
  try {
    applyView(await $fetch<any>(`${apiBase.value}/api/cost/admin/llm`, {
      method: "PUT", headers: fetchHeaders.value, body: { ...form },
    }));
    saved.value = true;
    usage.value = await $fetch<any>(`${apiBase.value}/api/cost/admin/llm/usage`,
                                    { headers: fetchHeaders.value });
  } catch (e: any) {
    error.value = e?.data?.detail || e?.message || "Не удалось сохранить";
  } finally {
    saving.value = false;
  }
}

async function test(): Promise<void> {
  testing.value = true;
  testResult.value = null;
  try {
    testResult.value = await $fetch<any>(`${apiBase.value}/api/cost/admin/llm/test`, {
      method: "POST", headers: fetchHeaders.value, body: {}, timeout: 90000,
    });
  } catch (e: any) {
    testResult.value = { ok: false, error: e?.data?.detail || e?.message || "сбой" };
  } finally {
    testing.value = false;
  }
}

async function loadModels(): Promise<void> {
  loadingModels.value = true;
  modelsError.value = "";
  try {
    const res: any = await $fetch(`${apiBase.value}/api/cost/admin/llm/models`,
                                  { headers: fetchHeaders.value, timeout: 40000 });
    models.value = res.models || [];
    if (!res.ok) modelsError.value = `Провайдер не отдал список: ${res.error || ""}`;
    else if (!models.value.length) modelsError.value = "Провайдер вернул пустой список";
  } catch (e: any) {
    modelsError.value = e?.data?.detail || e?.message || "Не удалось получить список";
  } finally {
    loadingModels.value = false;
  }
}

onMounted(load);
</script>

<style scoped>
.page { padding: var(--sp-5); max-width: 860px; }
.title { margin: 0 0 var(--sp-3); font-size: var(--fs-xl); color: var(--text-strong); }
.subtitle { margin: 0 0 var(--sp-3); font-size: var(--fs-md); color: var(--text-strong); }

.note {
  margin: 0 0 var(--sp-4);
  font-size: var(--fs-xs);
  line-height: var(--lh-relaxed);
  color: var(--text-muted);
}

.card {
  padding: var(--sp-4);
  margin-bottom: var(--sp-4);
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
}

.form { display: grid; gap: var(--sp-4); }
.row { display: grid; gap: var(--sp-1); }
.pair { display: grid; grid-template-columns: 1fr 1fr; gap: var(--sp-4); }

.lbl {
  font-size: var(--fs-xs);
  font-weight: var(--fw-medium);
  color: var(--text-secondary);
}

/* Откуда взято значение — «админка» или «окружение». Без этой пометки не
 * понять, почему поле пустое, а разбор работает. */
.src {
  margin-left: var(--sp-2);
  padding: 0 var(--sp-1);
  font-size: var(--fs-2xs);
  font-weight: var(--fw-regular);
  color: var(--text-muted);
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
}

.inp {
  padding: var(--sp-1) var(--sp-2);
  font-family: inherit;
  font-size: var(--fs-sm);
  color: var(--text-primary);
  background: var(--bg-surface-2);
  border: 1px solid var(--border);
  border-radius: var(--rd-2);
}
.inp:focus { outline: none; border-color: var(--border-focus); box-shadow: var(--shadow-focus); }
.inp.narrow { max-width: 220px; }

.model-row { display: flex; gap: var(--sp-2); align-items: center; }
.model-row .inp { flex: 1; min-width: 0; }

.switch { display: flex; gap: var(--sp-2); align-items: center; font-size: var(--fs-sm); }

.hint {
  font-size: var(--fs-2xs);
  line-height: var(--lh-relaxed);
  color: var(--text-muted);
}
.hint.warn, .warn { color: var(--warn); }

.actions { display: flex; gap: var(--sp-2); }

.btn {
  padding: var(--sp-1) var(--sp-4);
  font-family: inherit;
  font-size: var(--fs-sm);
  color: var(--accent-fg);
  background: var(--accent);
  border: 1px solid var(--accent);
  border-radius: var(--rd-2);
  cursor: pointer;
}
.btn:hover:not(:disabled) { background: var(--accent-hover); }
.btn:disabled { opacity: .5; cursor: default; }
.btn.ghost { color: var(--text-secondary); background: transparent; border-color: var(--border); }
.btn.ghost:hover:not(:disabled) { background: var(--bg-surface-2); }

.test { margin: 0; font-size: var(--fs-xs); line-height: var(--lh-relaxed); }
.ok { color: var(--pos); }
.cost-error { color: var(--neg); font-size: var(--fs-sm); }

.usage { width: 100%; border-collapse: collapse; font-size: var(--fs-xs); }
.usage th, .usage td {
  padding: var(--sp-1) var(--sp-2);
  text-align: right;
  border-bottom: 1px solid var(--border);
}
.usage th:first-child, .usage td:first-child { text-align: left; }
.usage td:not(:first-child) { font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
.usage tfoot td { border-bottom: none; }
</style>
