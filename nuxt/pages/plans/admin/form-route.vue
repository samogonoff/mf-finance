<!--
  Настройка маршрута формы (ТЗ МП §2.1, SPEC §28.4). Ключевая мысль ТЗ: маршрут —
  это ДАННЫЕ, а не код. Шаг «Финансист» между 1.2 и 1.3 включается здесь галочкой
  «включён», а не правкой Go; «пропускать при самосогласовании» закрывает
  вырожденный случай розницы РБ, где заполняющий и согласующий — одно лицо.

  Единица маршрута — форма (а не этап периода): крупные и мелкие МП, как и четыре
  страны розницы, ходят по маршруту независимо.
-->
<template>
  <div class="page-fr">
    <header class="page-header">
      <div>
        <h1 class="page-title">Маршрут форм</h1>
        <p class="page-subtitle">
          Шаги согласования по форме: кто, в каком порядке, к какому рабочему дню.
          Выключенный шаг в маршруте не участвует.
        </p>
      </div>
      <div class="page-actions">
        <NuxtLink to="/plans/admin/route" class="btn btn-ghost">
          <Icon name="lucide:git-branch" /> Маршрут процесса (этапы периода)
        </NuxtLink>
      </div>
    </header>

    <p v-if="!canEdit" class="banner banner-warn">
      Просмотр. Изменение маршрута — у роли «Администратор ТП».
    </p>
    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="note" class="banner banner-pos">{{ note }}</p>

    <div class="form-switch">
      <span class="fs-lbl">Форма</span>
      <div class="chip-row">
        <button
          v-for="f in FORMS"
          :key="f.code"
          type="button"
          class="chip"
          :class="{ active: formCode === f.code }"
          @click="pick(f.code)"
        >{{ f.title }} <span class="chip-code">{{ f.code }}</span></button>
      </div>
    </div>

    <SkeletonTable v-if="loading" :rows="5" :cols="6" :section-every="0" label="Загружаю маршрут формы" />

    <section v-else class="card">
      <div class="card-header">
        <span class="card-title">Шаги маршрута · {{ formTitle }}</span>
        <span class="hdr-hint">
          Включённых шагов: {{ enabledCount }} из {{ rows.length }}. Порядок задаётся полем «Порядок».
        </span>
      </div>
      <div class="table-scroll">
        <table class="data-table fr-table">
          <thead>
            <tr>
              <th class="th-on">Включён</th>
              <th class="th-code">Шаг</th>
              <th>Название</th>
              <th class="col-num th-ord">Порядок</th>
              <th class="th-kind">Вид</th>
              <th>Ответственные</th>
              <th class="col-num th-rd">Срок, р.д.</th>
              <th class="th-flag">Пропускать при самосогласовании</th>
              <th class="th-flag">Публиковать при утверждении</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in rows" :key="r.step_code" :class="{ off: !r.enabled, 'row-dirty': isDirty(r) }">
              <td>
                <label class="ck">
                  <input v-model="r.enabled" type="checkbox" :disabled="!canEdit" />
                  <span>{{ r.enabled ? "в маршруте" : "выключен" }}</span>
                </label>
              </td>
              <td class="cd">{{ r.step_code }}</td>
              <td>
                <input v-model="r.step_name" class="input" :disabled="!canEdit" placeholder="название шага" />
              </td>
              <td class="col-num">
                <NumberField
                  :model-value="r.sort_order"
                  :digits="0"
                  class="n-input"
                  :disabled="!canEdit"
                  @update:model-value="r.sort_order = $event ?? 0"
                />
              </td>
              <td>
                <select v-model="r.kind" class="select" :disabled="!canEdit">
                  <option value="fill">заполнение</option>
                  <option value="approve">согласование</option>
                  <option value="final">утверждение</option>
                </select>
              </td>
              <td>
                <input v-model="r.responsible" class="input" :disabled="!canEdit" placeholder="должность / роль по схеме" />
              </td>
              <td class="col-num">
                <NumberField
                  :model-value="r.due_rd"
                  :digits="0"
                  class="n-input"
                  :disabled="!canEdit"
                  @update:model-value="r.due_rd = $event ?? 0"
                />
              </td>
              <td>
                <label class="ck">
                  <input v-model="r.skip_if_same_user" type="checkbox" :disabled="!canEdit" />
                  <span>авто-проход</span>
                </label>
              </td>
              <td>
                <label class="ck">
                  <input v-model="r.publish_on_approve" type="checkbox" :disabled="!canEdit" />
                  <span>публикация</span>
                </label>
              </td>
              <td>
                <button class="btn btn-sm btn-primary" :disabled="!canEdit || busy || !isDirty(r)" @click="saveRow(r)">
                  <Icon name="lucide:save" /> Сохранить
                </button>
              </td>
            </tr>
            <tr v-if="!rows.length">
              <td colspan="10" class="empty-cell">
                Маршрут для этой формы не заведён — шаги создаются миграцией комплекта форм.
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="foot-hint">
        Вид шага определяет доступное действие: на «заполнении» — «Отправить на согласование»,
        на «согласовании» — «Согласовать», «утверждение» закрывает период на запись и (если
        отмечено) запускает публикацию в приёмник.
      </p>
    </section>
  </div>
</template>

<script setup lang="ts">
import NumberField from "~/components/NumberField.vue";
import SkeletonTable from "~/components/SkeletonTable.vue";
import { useMpConditions, type RouteStep } from "~/composables/useMpConditions";

definePageMeta({ middleware: ["scope-guard"] });

// Формы комплекта: реестр форм на сервере знает только эти два кода, поэтому
// селектор — фиксированный список, а не свободный ввод.
const FORMS = [
  { code: "TPL-MP", title: "Маркетплейсы" },
  { code: "TPL-TO-RETAIL", title: "Розница (ТО)" }
];

const api = useMpConditions();
const { hasRole, isAdmin } = useScope();
const canEdit = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));

const formCode = ref(FORMS[0].code);
const rows = ref<EditableStep[]>([]);
const loading = ref(true);
const busy = ref(false);
const error = ref("");
const note = ref("");

interface EditableStep extends RouteStep {
  _origin: string;
}

const formTitle = computed(() => FORMS.find((f) => f.code === formCode.value)?.title || formCode.value);
const enabledCount = computed(() => rows.value.filter((r) => r.enabled).length);

const signature = (r: RouteStep): string =>
  JSON.stringify([
    r.step_name, r.sort_order, r.kind, r.responsible,
    r.due_rd, r.enabled, r.skip_if_same_user, r.publish_on_approve
  ]);
const toRow = (r: RouteStep): EditableStep => ({ ...r, _origin: signature(r) });
const isDirty = (r: EditableStep) => signature(r) !== r._origin;

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
    const steps = await api.formRoute(formCode.value);
    rows.value = (steps || []).slice().sort((a, b) => a.sort_order - b.sort_order).map(toRow);
  } catch (e: unknown) {
    error.value = errText(e, "Не удалось загрузить маршрут формы");
  } finally {
    loading.value = false;
  }
};

const pick = (code: string) => {
  formCode.value = code;
  note.value = "";
  load();
};

const saveRow = async (r: EditableStep) => {
  busy.value = true;
  error.value = "";
  note.value = "";
  try {
    // Ручка принимает один шаг и возвращает весь маршрут — берём её ответ, чтобы
    // порядок на экране совпал с тем, по которому пойдут карточки.
    const steps = await api.saveFormRoute(formCode.value, {
      step_code: r.step_code,
      step_name: r.step_name,
      sort_order: r.sort_order,
      kind: r.kind,
      responsible: r.responsible,
      due_rd: r.due_rd,
      enabled: r.enabled,
      skip_if_same_user: r.skip_if_same_user,
      publish_on_approve: r.publish_on_approve
    });
    rows.value = (steps || []).slice().sort((a, b) => a.sort_order - b.sort_order).map(toRow);
    note.value = `Шаг ${r.step_code} сохранён: ${r.enabled ? "включён в маршрут" : "выключен"}.`;
  } catch (e: unknown) {
    error.value = errText(e, "Не удалось сохранить шаг маршрута");
  } finally {
    busy.value = false;
  }
};

onMounted(load);
</script>

<style scoped>
.page-fr { display: flex; flex-direction: column; gap: var(--sp-5); }
.banner { padding: var(--sp-4) var(--sp-5); border-radius: var(--rd-4); font-size: var(--fs-sm); margin: 0; }
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
.banner-pos { background: var(--pos-soft); color: var(--pos-strong); }
.banner-warn { background: var(--warn-soft); color: var(--warn); }

.form-switch { display: flex; align-items: center; gap: var(--sp-4); flex-wrap: wrap; }
.fs-lbl { font-size: var(--fs-2xs); text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-muted); }
.chip-row { display: flex; gap: var(--sp-2); flex-wrap: wrap; }
.chip {
  border: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text-secondary);
  border-radius: var(--rd-pill);
  padding: 3px 12px;
  font-size: var(--fs-sm);
  cursor: pointer;
}
.chip.active { background: var(--accent-soft); color: var(--accent); border-color: var(--accent); }
.chip-code { font-family: var(--font-mono); font-size: var(--fs-2xs); opacity: 0.7; margin-left: 4px; }

.hdr-hint, .foot-hint { font-size: var(--fs-2xs); color: var(--text-muted); }
.foot-hint { padding: var(--sp-4); margin: 0; }
.table-scroll { overflow-x: auto; }
.fr-table { min-width: 1100px; }
.th-on { width: 130px; }
.th-code { width: 70px; }
.th-ord { width: 90px; }
.th-kind { width: 140px; }
.th-rd { width: 100px; }
.th-flag { width: 130px; }
.cd { font-family: var(--font-mono); font-size: var(--fs-sm); color: var(--text-strong); }
.off td { opacity: 0.55; }
.row-dirty { background: var(--accent-soft); }
.ck { display: inline-flex; align-items: center; gap: 4px; font-size: var(--fs-2xs); color: var(--text-secondary); cursor: pointer; }
.n-input {
  width: 70px;
  text-align: right;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  padding: 3px 7px;
  background: var(--bg-surface);
  color: var(--text-primary);
}
.empty-cell { text-align: center; color: var(--text-muted); padding: var(--sp-7) 0; }
</style>
