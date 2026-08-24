<!--
  Полная форма ввода МП (task-driven), как прототип «Маркетплейсы_large»: строки =
  статьи P&L (продажи → %СПП → наценка → маржа розн/gross → COGS → прямые затраты →
  PL), колонки = площадки задания + Итого. Менеджер вводит ПРОДАЖИ с НДС (1046),
  правит %СПП/наценку и статьи затрат — производное считается ВЖИВУЮ (useMpCascade,
  зеркало go/internal/plans/mpform_calc.go). Факт/стратегия — read-only. ТЗ §7.2.3.

  Скорректированное ТЗ (§3.1, §3.4) добавило второе направление расчёта: в режиме
  inverse вводятся только продажи, а %СПП, наценки, себестоимость и суммы статей
  считаются из реестра «Условия площадки». Режим приходит с сервера в calc_mode —
  клиент его не выбирает, потому что закрытые периоды обязаны считаться тем
  алгоритмом, которым были утверждены.

  Здесь же живёт оболочка процесса (CardProcessPanel) и карточка площадки
  (PlatformCard, ТЗ §3.2): «провалиться в площадку, заполнить, вернуться».
-->
<template>
  <div class="page-mpf">
    <header class="page-header">
      <div>
        <h1 class="page-title">Форма МП · {{ segmentTitle }}</h1>
        <p class="page-subtitle">
          {{ form?.task.title }} · {{ form?.year }}-{{ String(form?.month || 0).padStart(2, "0") }} ·
          показано {{ visiblePlatforms.length }} из {{ form?.platforms.length || 0 }} площадок ·
          <span :title="'эффективная ставка первой площадки из справочника; расчёт идёт по ставке каждой площадки'">
            НДС {{ vatLabel }}
          </span> · {{ currency }} ·
          <span class="mode-tag" :class="{ inv: isInverse }" :title="modeHint">{{ modeLabel }}</span>
        </p>
      </div>
      <div class="page-actions">
        <NuxtLink :to="backLink" class="btn btn-ghost"><Icon name="lucide:arrow-left" /> К процессу</NuxtLink>
        <!-- Условия и общие затраты — соседние экраны того же процесса; без ссылок
             из формы их не найти, а в inverse без них форма не считается. -->
        <NuxtLink v-if="cardId" :to="`/plans/mp-conditions/${cardId}`" class="btn btn-sm btn-ghost">
          <Icon name="lucide:sliders-horizontal" /> Условия площадок
        </NuxtLink>
        <NuxtLink v-if="cardId" :to="`/plans/mp-common-costs/${cardId}`" class="btn btn-sm btn-ghost">
          <Icon name="lucide:layers" /> Общие затраты
        </NuxtLink>
        <button v-if="cardId" class="btn btn-sm btn-ghost" :disabled="checking" @click="doValidate">
          <Icon name="lucide:shield-check" :class="{ spin: checking }" /> Проверить
        </button>
        <label class="cur-switch" title="Валюта отображения (хранение — RUB)">
          <select v-model="currency" class="select select-sm" :disabled="dirty" @change="load">
            <option value="RUB">RUB</option>
            <option value="BYN">BYN</option>
            <option value="USD">USD</option>
          </select>
        </label>
        <button v-if="canEdit" class="btn btn-sm btn-ghost" :disabled="!hasStrategy" title="Копировать стратегический бюджет в тактику (TPL-09)" @click="copyStrategy"><Icon name="lucide:copy" /> Стратегия→тактика</button>
        <button v-if="canEdit" class="btn btn-sm btn-ghost" :disabled="!hasTarget" title="Копировать тактику-таргет (Budgeting) в форму" @click="copyTarget"><Icon name="lucide:copy-check" /> Таргет→тактика</button>
        <button class="btn btn-sm btn-ghost" @click="doExport"><Icon name="lucide:download" /> Экспорт</button>
        <button v-if="canEdit" class="btn btn-sm btn-ghost" @click="fileInput?.click()"><Icon name="lucide:upload" /> Импорт</button>
        <input ref="fileInput" type="file" accept=".xlsx" class="hidden-file" @change="doImport" />
        <button v-if="canEdit" class="btn btn-sm btn-primary" :disabled="saving" @click="save"><Icon name="lucide:save" :class="{ spin: saving }" /> Сохранить<span v-if="dirty" class="dirty-dot" title="есть несохранённые изменения">●</span></button>
      </div>
    </header>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>
    <p v-if="note" class="banner banner-pos">{{ note }}</p>
    <p v-if="form && !canEdit" class="banner banner-warn">Только просмотр — вы не исполнитель этого задания.</p>

    <!-- Оболочка процесса: статус карточки, шаги, возврат, публикация. Отправку
         на согласование блокируем по результату валидаций (ТЗ §7 — блокирующие
         замечания не пропускают форму дальше). -->
    <CardProcessPanel
      v-if="cardId"
      :card-id="cardId"
      :can-submit="canSubmit"
      :submit-hint="submitHint"
    />

    <section v-if="checkResult" class="card check-card">
      <div class="card-header">
        <span class="card-title">
          Проверка формы — {{ checkResult.issues.length ? `замечаний: ${checkResult.issues.length}` : "замечаний нет" }}
        </span>
        <span class="badge" :class="checkResult.can_submit ? 'badge-pos' : 'badge-neg'">
          {{ checkResult.can_submit ? "можно отправлять" : "отправка заблокирована" }}
        </span>
      </div>
      <table v-if="checkResult.issues.length" class="data-table compact">
        <thead><tr><th class="th-code">Код</th><th>Площадка</th><th>Замечание</th></tr></thead>
        <tbody>
          <tr v-for="(i, idx) in checkResult.issues" :key="idx" :class="i.level === 'blocking' ? 'row-block' : 'row-warn'">
            <td><span class="badge" :class="i.level === 'blocking' ? 'badge-neg' : 'badge-warn'">{{ i.code }}</span></td>
            <td>
              <span v-if="i.name_cfo || i.code_cfo">{{ i.name_cfo || "—" }}</span>
              <span v-else class="muted">форма целиком</span>
              <span v-if="i.code_cfo" class="cfo-inline">ЦФО {{ i.code_cfo }}</span>
            </td>
            <td>
              {{ i.message }}
              <!-- ТЗ §6.1: отклонение сверх порога само не блокирует, но требует
                   обоснования — иначе согласующий не поймёт, что изменилось. -->
              <span v-if="i.needs_why" class="need-note">требуется комментарий (обоснование в реестре условий)</span>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else class="check-ok">Блокирующих замечаний нет — форму можно отправлять на согласование.</p>
      <p v-if="checkResult.needs_reason?.length" class="check-why">
        Без обоснования останутся {{ checkResult.needs_reason.length }} изменений условий —
        <NuxtLink v-if="cardId" :to="`/plans/mp-conditions/${cardId}`" class="link">заполнить причины</NuxtLink>.
      </p>
    </section>

    <!-- Одна форма на все МП: фильтры сужают показ, ввод по скрытым площадкам
         сохраняется как есть (фильтр — только представление). -->
    <div v-if="form && form.platforms.length > 1" class="mp-filters">
      <div class="mf">
        <span class="mf-lbl">Маркет</span>
        <select v-model="platformFilter" class="select select-sm">
          <option value="0">все площадки</option>
          <option v-for="p in form.platforms" :key="p.code_cfo" :value="String(p.code_cfo)">
            {{ p.name || p.code_cfo }}
          </option>
        </select>
      </div>
      <div v-if="segments.length > 1" class="mf">
        <span class="mf-lbl">Сегмент</span>
        <div class="chip-row">
          <button type="button" class="chip" :class="{ active: segmentFilter === '' }" @click="segmentFilter = ''">все</button>
          <button
            v-for="s in segments"
            :key="s"
            type="button"
            class="chip"
            :class="{ active: segmentFilter === s }"
            @click="segmentFilter = s"
          >{{ s === "large" ? "крупные" : "мелкие" }}</button>
        </div>
      </div>
      <div class="mf">
        <span class="mf-lbl">ЮЛ</span>
        <select v-model="legalFilter" class="select select-sm">
          <option value="">все ЮЛ</option>
          <option v-for="le in legalEntities" :key="le" :value="le">{{ le }}</option>
        </select>
      </div>
    </div>

    <div v-if="form" class="legend">
      <span class="lg lg-input">ввод</span>
      <span class="lg lg-calced">расчёт (правится)</span>
      <span class="lg lg-calc">расчёт</span>
      <span v-if="isInverse" class="lg-hint">
        Режим «расчёт от условий»: руками вводятся только продажи с НДС. %СПП, наценки и
        себестоимость правятся в реестре условий; сумму статьи затрат можно переопределить
        вручную — автопересчёт её не затрёт.
      </span>
      <span v-else class="lg-hint">Менеджер задаёт «Продажи с НДС», %СПП и статьи затрат — остальное считается автоматически.</span>
    </div>

    <!-- Первая загрузка: контур будущей таблицы. Форма тянет факт/стратегию/таргет
         из OLAP — это секунды, и пустой экран всё это время выглядит как зависание. -->
    <div v-if="!form && loading" class="card form-card">
      <p class="loading-line">
        <Icon name="lucide:loader-circle" class="spin" />
        Загружаю форму задания: факт, стратегия и таргет из Budgeting…
      </p>
      <SkeletonTable :rows="16" :cols="5" :section-every="5" label="Загружаю форму МП" />
    </div>

    <div v-if="form" class="card form-card" :class="{ busy: loading }">
      <!-- Перезагрузка при смене валюты: таблицу не убираем, но помечаем занятой. -->
      <div v-if="loading" class="busy-veil"><Icon name="lucide:loader-circle" class="spin" /> Пересчитываю в {{ currency }}…</div>
      <div class="table-wrap">
        <table class="data-table mp-grid">
          <thead>
            <!-- Группировка колонок по сегменту — когда форма покрывает и крупные, и мелкие МП. -->
            <tr v-if="segmentGroups.length > 1" class="grp-row">
              <th class="col-line"></th>
              <th v-for="g in segmentGroups" :key="g.segment" :colspan="g.count" class="grp-head">
                {{ g.segment === "large" ? "Крупные МП" : "Мелкие МП" }}
              </th>
              <th></th>
            </tr>
            <tr>
              <th class="col-line">Показатель</th>
              <!-- Заголовок колонки — вход в карточку площадки (ТЗ §3.2): в узкой
                   колонке не видно ни условий, ни сравнения со сценариями. -->
              <th v-for="p in visiblePlatforms" :key="p.code_cfo" class="col-plat num">
                <button type="button" class="plat-btn" :title="`Открыть карточку площадки «${p.name || p.code_cfo}»`" @click="openPlatform(p)">
                  {{ p.name || p.code_cfo }}
                  <Icon name="lucide:maximize-2" class="plat-ic" />
                </button>
                <span class="cfo">ЦФО {{ p.code_cfo }}</span>
              </th>
              <th class="col-total num">Итого<span v-if="totalScoped" class="cfo">по фильтру</span></th>
            </tr>
          </thead>
          <tbody>
            <template v-for="sec in sections" :key="sec.name">
              <tr class="sec-row"><td :colspan="visiblePlatforms.length + 2">{{ sec.name }}</td></tr>
              <tr v-for="line in sec.lines" :key="line.block_type" class="line-row" :class="lineClass(line)">
                <td class="col-line">
                  <span class="line-name">{{ line.name }}</span>
                  <span v-if="line.code_pl" class="cp">PL {{ line.code_pl }}</span>
                  <Icon v-if="line.formula" name="lucide:info" class="fi" :title="line.formula" />
                </td>

                <!-- ячейки по площадкам -->
                <td v-for="p in visiblePlatforms" :key="p.code_cfo" class="cell" :class="cellClass(line, p.code_cfo)">
                  <template v-if="line.scope === 'total'">
                    <span class="muted">—</span>
                  </template>
                  <template v-else-if="editableCell(line)">
                    <div class="cell-row">
                      <!-- Проценты вводятся в процентах (31), хранятся долей (0.31). -->
                      <NumberField
                        :model-value="displayInput(line, p.code_cfo)"
                        class="cell-input"
                        :class="{ pct: line.value_kind === 'pct' }"
                        :disabled="!canEdit" :placeholder="line.value_kind === 'pct' ? '%' : '—'"
                        @update:model-value="onInput(line, p.code_cfo, $event)"
                      />
                      <span v-if="line.value_kind === 'pct'" class="pct-sign">%</span>
                      <button v-if="canEdit && line.kind === 'input'" class="corr-btn" :class="{ on: isCorr(p.code_cfo, line.block_type) }" title="Корректировка с причиной (ADJ-02)" @click="toggleCorr(p.code_cfo, line.block_type)"><Icon name="lucide:pencil" /></button>
                    </div>
                    <span v-if="line.kind === 'input' && factOf(p.code_cfo, line.block_type) != null" class="fact">факт: {{ fmtMoney(factOf(p.code_cfo, line.block_type)) }}</span>
                    <span v-if="prevOf(p.code_cfo, line.block_type) != null" class="prev">пр. год: {{ fmt(line, prevOf(p.code_cfo, line.block_type)) }}</span>
                    <span v-if="stratOf(p.code_cfo, line.block_type) != null" class="strat">страт: {{ fmt(line, stratOf(p.code_cfo, line.block_type)) }}</span>
                    <span v-if="targetOf(p.code_cfo, line.block_type) != null" class="target">таргет: {{ fmt(line, targetOf(p.code_cfo, line.block_type)) }}</span>
                    <!-- В inverse суммы статей — производные от доли: показываем и долю
                         из условий, и посчитанную по ней сумму. Пустое поле = «берём
                         расчёт», заполненное = ручное переопределение (ТЗ §7.2). -->
                    <span v-if="isInverse && line.cost_line" class="share">доля {{ fmtPct(condShare(line, p.code_cfo)) }}</span>
                    <span v-if="isInverse && line.cost_line && displayInput(line, p.code_cfo) == null" class="calc-hint">
                      по доле: {{ fmtMoney(cellValue(line, p.code_cfo)) }}
                    </span>
                    <input v-if="isCorr(p.code_cfo, line.block_type)" v-model="corr[key(p.code_cfo, line.block_type)]" class="corr-reason" :disabled="!canEdit" placeholder="причина корректировки…" />
                  </template>
                  <template v-else>
                    <span class="calc-val" :class="{ neg: cellValue(line, p.code_cfo) < 0 }">{{ fmt(line, cellValue(line, p.code_cfo)) }}</span>
                    <!-- Доля статьи: в inverse она ЗАДАНА условиями площадки, поэтому
                         показываем её из реестра, а не считаем обратно от суммы. -->
                    <span v-if="line.cost_line" class="share">доля {{ fmtPct(isInverse ? condShare(line, p.code_cfo) : shareOf(line, p.code_cfo)) }}</span>
                  </template>
                </td>

                <!-- Итого -->
                <td class="cell total-cell">
                  <template v-if="line.scope === 'total' && editableCell(line)">
                    <NumberField
                      :model-value="totals[line.block_type] ?? null"
                      class="cell-input" :disabled="!canEdit" placeholder="—"
                      @update:model-value="onTotalInput(line.block_type, $event)"
                    />
                  </template>
                  <template v-else>
                    <span class="calc-val" :class="{ neg: totalValue(line) < 0 }">{{ fmt(line, totalValue(line)) }}</span>
                  </template>
                </td>
              </tr>
            </template>
            <tr v-if="!form.platforms.length"><td :colspan="2" class="empty-cell">У задания нет площадок (ЦФО не размечены).</td></tr>
          </tbody>

          <!-- Итоги формы. Считаются по ВСЕМ площадкам задания, а не по фильтру:
               дефект прототипа (доля ПЗ в обороте 27,92 % вместо 26,32 % из-за
               того, что комиссии вычитались только у WB и Ozon) не воспроизводится
               — ТЗ §1 п.25, отдельный пункт приёмки. -->
          <tfoot v-if="form.platforms.length">
            <tr class="tot-row">
              <td class="col-line">PL формы (сумма)</td>
              <td :colspan="visiblePlatforms.length" class="tot-note">
                = цена площадки без НДС − себестоимость отпускная − общие затраты − прямые затраты + комиссии
              </td>
              <td class="total-cell strong">{{ fmtMoney(formTotals.pl) }}</td>
            </tr>
            <tr class="tot-row">
              <td class="col-line">PL формы, %</td>
              <td :colspan="visiblePlatforms.length" class="tot-note">от цены площадки без НДС</td>
              <td class="total-cell strong">{{ fmtPct(formTotals.plPct) }}</td>
            </tr>
            <tr class="tot-row">
              <td class="col-line">Доля прямых затрат в обороте</td>
              <td :colspan="visiblePlatforms.length" class="tot-note">
                по всем {{ form.platforms.length }} площадкам формы (не по фильтру); комиссии вычитаются у каждой площадки
              </td>
              <td class="total-cell strong">{{ fmtPct(formTotals.directShareTurnover) }}</td>
            </tr>
            <tr class="tot-row">
              <td class="col-line">Общие затраты по МП</td>
              <td :colspan="visiblePlatforms.length" class="tot-note">
                <template v-if="cardId">
                  вводятся отдельно —
                  <NuxtLink :to="`/plans/mp-common-costs/${cardId}`" class="link">7 групп статей PL</NuxtLink>
                  <span v-if="commonCostsError" class="tot-warn">· {{ commonCostsError }}</span>
                </template>
                <template v-else>карточка процесса не создана — общие затраты не подключены</template>
              </td>
              <td class="total-cell">{{ fmtMoney(commonCostTotal) }}</td>
            </tr>
          </tfoot>
        </table>
      </div>
    </div>

    <!-- Карточка площадки: тот же ввод, что в сетке, но с условиями и сценариями. -->
    <PlatformCard
      :open="!!platformModal"
      :platform="platformModal"
      :lines="form?.lines || []"
      :cells="form?.cells || []"
      :values="platformModal ? platformValues[platformModal.code_cfo] || {} : {}"
      :inputs="platformModal ? platformInputs(platformModal.code_cfo) : {}"
      :conditions="platformModal ? condOf(platformModal.code_cfo) : null"
      :inverse="isInverse"
      :editable="canEdit"
      :currency="currency"
      :vat-fallback="form?.vat || 0.2"
      :card-id="cardId"
      @close="platformModal = null"
      @input="onPlatformInput"
    />
  </div>
</template>

<script setup lang="ts">
import { useTasks, type MpTaskForm, type MpLine, type MpSaveRow, type MpFormCell, type MpFormPlatform } from "~/composables/useTasks";
import {
  computePlatform,
  computePlatformInverse,
  computeFormTotals,
  vatByCountry,
  B,
  type MpConditions
} from "~/composables/useMpCascade";
import { useMpConditions, type MpIssue } from "~/composables/useMpConditions";
import { num } from "~/utils/format";
import NumberField from "~/components/NumberField.vue";
import CardProcessPanel from "~/components/plans/CardProcessPanel.vue";
import PlatformCard from "~/components/plans/PlatformCard.vue";

definePageMeta({ middleware: "scope-guard" });

const route = useRoute();
const taskId = Number(route.params.taskId);
const api = useTasks();
const cardApi = useMpConditions();
const { user } = useAuth();
const { hasRole, isAdmin } = useScope();

const form = ref<MpTaskForm | null>(null);
const inputs = reactive<Record<string, number | null>>({});      // ключ cfo:block → ввод (площадочные editable)
const totals = reactive<Record<string, number | null>>({});      // block → ввод (тотал-строки)
const corr = reactive<Record<string, string>>({});               // ключ ячейки → причина корректировки
const error = ref("");
const note = ref("");
const saving = ref(false);
const loading = ref(true); // сразу true: onMounted грузит форму, скелетон не должен мигать
const dirty = ref(false);
// Валюта отображения; хранение тактики всегда в RUB (пересчёт делает сервер).
// Переключение заблокировано при несохранённых правках — иначе они потеряются.
const currency = ref("RUB");
const fileInput = ref<HTMLInputElement | null>(null);

// ── Режим расчёта, карточка процесса, валидации ──
const isInverse = computed(() => form.value?.calc_mode === "inverse");
const cardId = computed(() => form.value?.card_id || 0);
const modeLabel = computed(() => (isInverse.value ? "расчёт от условий" : "расчёт от сумм"));
const modeHint = computed(() =>
  isInverse.value
    ? "Инверсия ТЗ §3.1: продажи + условия площадки → расходная часть"
    : "Прежнее направление: суммы → доли и наценки"
);
const checking = ref(false);
const checkResult = ref<{ issues: MpIssue[]; blocking: boolean; needs_reason: MpIssue[]; can_submit: boolean } | null>(null);
// До первой проверки отправку не блокируем: иначе форма без загруженных валидаций
// выглядела бы сломанной. Проверка сама выставляет can_submit.
const canSubmit = computed(() => (checkResult.value ? checkResult.value.can_submit : true));
const submitHint = computed(() => {
  if (!checkResult.value || checkResult.value.can_submit) return "";
  if (checkResult.value.blocking) return "Есть блокирующие замечания — устраните их и проверьте снова";
  return "Часть изменений условий без обоснования — заполните причины в реестре условий";
});

// Общие затраты (7 групп статей PL) в PL формы не считаются от продаж, а вводятся
// на отдельном экране; здесь нужен только их итог.
const commonCostTotal = ref(0);
const commonCostsError = ref("");

// Карточка площадки (ТЗ §3.2).
const platformModal = ref<MpFormPlatform | null>(null);
const openPlatform = (p: MpFormPlatform) => { platformModal.value = p; };
const platformInputs = (cfo: number): Record<string, number | null> => {
  const out: Record<string, number | null> = {};
  for (const l of form.value?.lines || []) {
    if (l.scope === "platform") out[l.block_type] = inputs[key(cfo, l.block_type)] ?? null;
  }
  return out;
};
const onPlatformInput = (block: string, v: number | null) => {
  if (!platformModal.value) return;
  inputs[key(platformModal.value.code_cfo, block)] = v;
  dirty.value = true;
};

const key = (cfo: number, block: string) => `${cfo}:${block}`;
const lineMap = computed<Record<string, MpLine>>(() => {
  const m: Record<string, MpLine> = {};
  for (const l of form.value?.lines || []) m[l.block_type] = l;
  return m;
});
// Строки, чей ввод сидируется из вычисленного значения (а не из факта): %СПП, наценка.
const SEED_FROM_VALUE = new Set([B.spp, B.markup]);

const sections = computed(() => {
  const out: { name: string; lines: MpLine[] }[] = [];
  for (const l of form.value?.lines || []) {
    if (l.kind === "header") continue;
    let s = out.find((x) => x.name === l.section);
    if (!s) { s = { name: l.section, lines: [] }; out.push(s); }
    s.lines.push(l);
  }
  return out;
});

// Одна форма на все МП (миграция 0029): площадки обоих сегментов приходят одним
// заданием, фильтры ниже — только представление, они не влияют на сохранение.
const platformFilter = ref("0");
const segmentFilter = ref("");
const legalFilter = ref("");

const segments = computed(() =>
  [...new Set((form.value?.platforms || []).map((p) => p.segment).filter(Boolean))].sort()
);
const legalEntities = computed(() =>
  [...new Set((form.value?.platforms || []).map((p) => p.legal_entity).filter(Boolean))].sort()
);
const visiblePlatforms = computed(() =>
  (form.value?.platforms || []).filter((p) => {
    if (platformFilter.value !== "0" && String(p.code_cfo) !== platformFilter.value) return false;
    if (segmentFilter.value && p.segment !== segmentFilter.value) return false;
    if (legalFilter.value && p.legal_entity !== legalFilter.value) return false;
    return true;
  })
);
const totalScoped = computed(() => visiblePlatforms.value.length !== (form.value?.platforms.length || 0));
// Ставка НДС выводится с двумя знаками: у площадок она ЭФФЕКТИВНАЯ и не равна
// законодательной (20,36 % у WB/Lamoda/Ozon, 16,62 % у Yandex Market — ТЗ §3.4).
// Округление до целых стирало бы это отличие.
const vatLabel = computed(() => `${((form.value?.vat || 0) * 100).toFixed(2).replace(".", ",")} %`);

const segmentTitle = computed(() => {
  if (segments.value.length > 1) return "все площадки";
  return segments.value[0] === "small" ? "мелкие МП" : "крупные МП";
});
// Порядок колонок = порядок площадок; группы считаем по соседним одинаковым сегментам.
const segmentGroups = computed(() => {
  const out: { segment: string; count: number }[] = [];
  for (const p of visiblePlatforms.value) {
    const last = out[out.length - 1];
    if (last && last.segment === p.segment) last.count++;
    else out.push({ segment: p.segment, count: 1 });
  }
  return out;
});

const editableCell = (line: MpLine) => canEdit.value && line.editable;
const canEdit = computed(() => {
  if (isAdmin.value || hasRole("ROLE_PLANS_ADMIN")) return true;
  const uid = user.value?.id;
  const t = form.value?.task;
  return !!t && (t.assignee_user_id === uid || t.delegate_user_id === uid);
});
const backLink = computed(() => (form.value ? `/plans/process/${form.value.task.pl_id}` : "/plans"));

// Условия площадки (приходят только в inverse-режиме).
const condOf = (cfo: number): MpConditions | null => form.value?.conditions?.[cfo] ?? null;
// Доля статьи из условий — она ЗАДАНА, а не выведена из суммы.
const condShare = (line: MpLine, cfo: number): number => condOf(cfo)?.shares?.[line.block_type] ?? 0;

// Живой пересчёт каскада по каждой площадке из введённых значений.
// Направление берём из calc_mode карточки: в inverse расходная часть считается из
// условий площадки, а введённые суммы статей работают как переопределения
// (зеркало go/internal/plans/task_mpform.go → platformValues).
const platformValues = computed<Record<number, Record<string, number>>>(() => {
  const map: Record<number, Record<string, number>> = {};
  for (const p of form.value?.platforms || []) {
    const inp: Record<string, number | null | undefined> = {};
    for (const l of form.value?.lines || []) {
      if (l.scope === "platform" && l.editable) inp[l.block_type] = inputs[key(p.code_cfo, l.block_type)];
    }
    const vat = vatByCountry(p.country);
    const cond = condOf(p.code_cfo);
    map[p.code_cfo] = isInverse.value && cond ? computePlatformInverse(inp, cond, vat) : computePlatform(inp, vat);
  }
  return map;
});

// Итоги формы — по ВСЕМ площадкам задания (ТЗ §1 п.25), включая скрытые фильтром.
const formTotals = computed(() =>
  computeFormTotals(
    (form.value?.platforms || []).map((p) => platformValues.value[p.code_cfo] || {}),
    commonCostTotal.value
  )
);

const cellValue = (line: MpLine, cfo: number): number => platformValues.value[cfo]?.[line.block_type] ?? 0;
// Итог считается по ВИДИМЫМ площадкам: отфильтровав срез, пользователь ждёт итог
// именно по нему. Каскад при этом считается по всем — ввод скрытых не теряется.
const sumBlock = (block: string): number =>
  visiblePlatforms.value.reduce((s, p) => s + (platformValues.value[p.code_cfo]?.[block] ?? 0), 0);

// Итого по строке: сумма денег; проценты пересчитываются из агрегатов.
const totalValue = (line: MpLine): number => {
  if (line.scope === "total") return Number(totals[line.block_type]) || 0;
  if (line.value_kind === "money") return sumBlock(line.block_type);
  const net = sumBlock(B.salesPlatNet);
  switch (line.block_type) {
    case B.spp: { const mgr = sumBlock(B.salesManagerNet); return mgr === 0 ? 0 : 1 - net / mgr; }
    case B.markup: { const c = sumBlock(B.shipments); return c === 0 ? 0 : net / c - 1; }
    case B.markupTotal: { const c = sumBlock(B.cogsTotal); return c === 0 ? 0 : net / c - 1; }
    case B.retailMarginPct: return net === 0 ? 0 : sumBlock(B.retailMargin) / net;
    case B.grossMarginPct: return net === 0 ? 0 : sumBlock(B.grossMargin) / net;
    case B.plPlatformPct: return net === 0 ? 0 : sumBlock(B.plPlatform) / net;
    case B.directShare: { const mgr = sumBlock(B.salesManagerNet); return mgr === 0 ? 0 : sumBlock(B.platformCosts) / mgr; }
    default: return 0;
  }
};

// Доля статьи затрат в выручке по ценам менеджера (без НДС).
const shareOf = (line: MpLine, cfo: number): number => {
  const mgrNet = platformValues.value[cfo]?.[B.salesManagerNet] ?? 0;
  return mgrNet === 0 ? 0 : cellValue(line, cfo) / mgrNet;
};

const cellRef = (cfo: number, block: string) => form.value?.cells.find((c) => c.code_cfo === cfo && c.block_type === block);
const factOf = (cfo: number, block: string): number | null => cellRef(cfo, block)?.fact ?? null;
const prevOf = (cfo: number, block: string): number | null => cellRef(cfo, block)?.fact_prev ?? null;
const stratOf = (cfo: number, block: string): number | null => cellRef(cfo, block)?.strategy ?? null;
const targetOf = (cfo: number, block: string): number | null => cellRef(cfo, block)?.target ?? null;
const hasStrategy = computed(() => !!form.value?.cells.some((c) => c.strategy != null));
const hasTarget = computed(() => !!form.value?.cells.some((c) => c.target != null));

// Ввод процентов — в процентах: в поле 31, в модели 0.31 (финансисты вводят «31»,
// а не долю). Округление гасит артефакты float при ×100.
const displayInput = (line: MpLine, cfo: number): number | null => {
  const v = inputs[key(cfo, line.block_type)];
  if (v == null || Number.isNaN(v)) return null;
  return line.value_kind === "pct" ? Number((v * 100).toFixed(4)) : v;
};
// NumberField отдаёт уже разобранное число (округление до копеек — на blur).
const onInput = (line: MpLine, cfo: number, v: number | null) => {
  const k = key(cfo, line.block_type);
  inputs[k] = v == null ? null : line.value_kind === "pct" ? Number((v / 100).toFixed(6)) : v;
  dirty.value = true;
};
const onTotalInput = (block: string, v: number | null) => {
  totals[block] = v;
  dirty.value = true;
};

const isCorr = (cfo: number, block: string) => key(cfo, block) in corr;
const toggleCorr = (cfo: number, block: string) => {
  const k = key(cfo, block);
  if (k in corr) delete corr[k]; else corr[k] = "";
};

const lineClass = (line: MpLine) => `k-${line.kind}` + (line.block_type === B.plPlatform || line.block_type === B.platformCosts ? " strong" : "");
const cellClass = (line: MpLine, cfo: number) => (isCorr(cfo, line.block_type) ? "is-corr" : "");

// Форматирование по типу строки. 2 знака везде — как в полях ввода (NumberField),
// иначе подсказка «факт» и значение в поле над ней расходятся на копейки.
const fmtMoney = (v: number | null | undefined) => (v == null || Number.isNaN(v) ? "—" : num(v, 2));
const fmtPct = (v: number | null | undefined) => (v == null || Number.isNaN(v) ? "—" : num(v * 100, 2) + "%");
const fmt = (line: MpLine, v: number | null | undefined) => (line.value_kind === "pct" ? fmtPct(v) : fmtMoney(v));

const seedFromCells = () => {
  for (const k of Object.keys(inputs)) delete inputs[k];
  for (const k of Object.keys(totals)) delete totals[k];
  for (const c of form.value?.cells || []) {
    const line = lineMap.value[c.block_type];
    if (!line?.editable) continue;
    if (line.scope === "total") {
      totals[c.block_type] = c.tactic ?? c.value ?? 0;
    } else if (isInverse.value && line.kind === "calc_editable") {
      // Суммы статей в inverse — расчётные. Сеять их фактом нельзя: тогда каждая
      // статья сразу стала бы «ручным переопределением» и доля из условий никогда
      // бы не применилась. Переопределением считается только сохранённая тактика
      // (так же решает сервер: overrides берутся из tactic, не из факта).
      inputs[key(c.code_cfo, c.block_type)] = c.tactic ?? null;
      if (c.is_manual) corr[key(c.code_cfo, c.block_type)] = c.reason || "";
    } else {
      const seed = c.tactic ?? (SEED_FROM_VALUE.has(c.block_type) ? c.value : c.fact) ?? 0;
      inputs[key(c.code_cfo, c.block_type)] = seed;
      if (c.is_manual) corr[key(c.code_cfo, c.block_type)] = c.reason || "";
    }
  }
};

// Перенос read-only сценария в тактику: стратегия (TPL-09) или таргет из Budgeting.
const copyScenario = (pick: (c: MpFormCell) => number | null, what: string) => {
  if (!form.value) return;
  let cnt = 0;
  for (const c of form.value.cells) {
    const v = pick(c);
    if (v == null) continue;
    const line = lineMap.value[c.block_type];
    if (!line?.editable) continue;
    if (line.scope === "total") totals[c.block_type] = v;
    else inputs[key(c.code_cfo, c.block_type)] = v;
    cnt++;
  }
  if (cnt) { dirty.value = true; note.value = `Скопировано из «${what}»: ${cnt} значений`; }
};
const copyStrategy = () => copyScenario((c) => c.strategy, "стратегия");
const copyTarget = () => copyScenario((c) => c.target, "таргет");

const load = async () => {
  loading.value = true;
  try {
    form.value = await api.mpForm(taskId, currency.value);
    seedFromCells();
    dirty.value = false;
    await loadCommonCosts();
  } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка загрузки формы"; }
  finally { loading.value = false; }
};

// Итог общих затрат нужен только для строки PL формы. Недоступность этой ручки не
// должна ломать ввод: показываем ноль и подпись, что итог не подтянулся.
const loadCommonCosts = async () => {
  commonCostTotal.value = 0;
  commonCostsError.value = "";
  if (!cardId.value) return;
  try {
    const cc = await cardApi.commonCosts(cardId.value);
    commonCostTotal.value = (cc.values || []).reduce((s, v) => s + (v.amount || 0), 0);
  } catch {
    commonCostsError.value = "итог общих затрат не загрузился";
  }
};

const doValidate = async () => {
  if (!cardId.value) return;
  checking.value = true;
  error.value = "";
  note.value = "";
  try {
    checkResult.value = await cardApi.validate(cardId.value);
    if (checkResult.value.can_submit) note.value = "Проверка пройдена: блокирующих замечаний нет.";
  } catch (e: unknown) {
    const d = typeof e === "object" && e && "data" in e ? (e as { data?: { error?: string } }).data : null;
    error.value = d?.error || (e instanceof Error ? e.message : "Проверка не выполнена");
  } finally {
    checking.value = false;
  }
};

const save = async () => {
  if (!form.value) return;
  saving.value = true; error.value = ""; note.value = "";
  const rows: MpSaveRow[] = [];
  for (const line of form.value.lines) {
    if (!line.editable) continue;
    if (line.scope === "total") {
      const v = totals[line.block_type];
      if (v != null && !Number.isNaN(v)) rows.push({ code_cfo: 0, block_type: line.block_type, code_pl: line.code_pl, amount: Number(v), is_manual: false, comment: "" });
      continue;
    }
    for (const p of form.value.platforms) {
      const k = key(p.code_cfo, line.block_type);
      const v = inputs[k];
      if (v == null || Number.isNaN(v)) continue;
      const manual = k in corr;
      if (manual && !corr[k].trim()) { error.value = `Укажите причину корректировки (${p.name || p.code_cfo} · ${line.name})`; saving.value = false; return; }
      rows.push({ code_cfo: p.code_cfo, block_type: line.block_type, code_pl: line.code_pl, amount: Number(v), is_manual: manual, comment: manual ? corr[k].trim() : "" });
    }
  }
  try {
    await api.saveMpForm(taskId, rows, currency.value);
    note.value = `Сохранено ${rows.length} значений`;
    dirty.value = false;
  } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка сохранения"; }
  finally { saving.value = false; }
};

const doExport = async () => {
  try {
    const blob = await api.exportMpForm(taskId);
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url; a.download = `TPL-MP_task${taskId}.xlsx`; a.click();
    URL.revokeObjectURL(url);
  } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка экспорта"; }
};
const doImport = async (ev: Event) => {
  const f = (ev.target as HTMLInputElement).files?.[0];
  if (!f) return;
  error.value = ""; note.value = "";
  try {
    const r = await api.importMpForm(taskId, f);
    note.value = `Импортировано ${r.imported} значений`;
    await load();
  } catch (e) { error.value = e instanceof Error ? e.message : "Ошибка импорта"; }
  finally { if (fileInput.value) fileInput.value.value = ""; }
};

onMounted(load);
</script>

<style scoped>
.banner { padding: var(--sp-4) var(--sp-5); border-radius: var(--rd-4, 6px); margin-bottom: var(--sp-5); font-size: var(--fs-sm); }
.banner-neg { background: var(--neg-soft); color: var(--neg-strong); }
.banner-pos { background: var(--pos-soft); color: var(--pos-strong); }
.banner-warn { background: var(--warn-soft); color: var(--warn); }
.page-actions { display: flex; gap: var(--sp-3); flex-wrap: wrap; }

.legend { display: flex; align-items: center; gap: var(--sp-3); margin-bottom: var(--sp-4); font-size: var(--fs-2xs); flex-wrap: wrap; }
.lg { padding: 1px 8px; border-radius: 999px; font-weight: var(--fw-medium); }
.lg-input { background: var(--accent-soft, #eef); color: var(--accent); }
.lg-calced { background: var(--warn-soft); color: var(--warn); }
.lg-calc { background: var(--bg-tonal); color: var(--text-secondary); }
.lg-hint { color: var(--text-muted); }

.form-card { padding: 0; overflow: hidden; position: relative; }
.form-card.busy .table-wrap { opacity: 0.45; pointer-events: none; transition: opacity 0.15s ease; }
.busy-veil {
  position: absolute; inset: 0; z-index: 3;
  display: flex; align-items: flex-start; justify-content: center;
  padding-top: var(--sp-7); gap: 6px;
  font-size: var(--fs-sm); color: var(--text-secondary);
}
.loading-line {
  display: flex; align-items: center; gap: 6px;
  padding: var(--sp-4) var(--sp-4) 0;
  font-size: var(--fs-sm); color: var(--text-secondary);
}
.mp-grid { border-collapse: collapse; width: 100%; }
.mp-grid th, .mp-grid td { vertical-align: top; }
.col-line { min-width: 300px; position: sticky; left: 0; background: var(--bg-surface); z-index: 1; }
.col-plat, .col-total { min-width: 176px; text-align: right; }
/* --fw-regular, а не несуществующий --fw-normal: с нерезолвящимся токеном
   свойство отбрасывается и код ЦФО наследует жирный шрифт заголовка колонки,
   переставая быть вторичным атрибутом. */
.cfo { display: block; font-size: var(--fs-2xs); color: var(--text-muted); font-family: var(--font-mono); font-weight: var(--fw-regular); }

.sec-row td { background: var(--bg-tonal); font-weight: var(--fw-bold); font-size: var(--fs-xs); text-transform: uppercase; letter-spacing: .04em; color: var(--text-secondary); padding: var(--sp-2) var(--sp-4); position: sticky; left: 0; }
.line-row td { border-top: 1px solid var(--border); padding: var(--sp-2) var(--sp-4); }
.line-row.strong td { border-top: 2px solid var(--border-strong); font-weight: var(--fw-bold); }
.line-name { font-size: var(--fs-sm); }
.cp { display: inline-block; margin-left: 6px; font-size: var(--fs-2xs); color: var(--text-muted); font-family: var(--font-mono); }
.fi { margin-left: 4px; color: var(--text-muted); width: 13px; height: 13px; cursor: help; vertical-align: middle; }

.cell { text-align: right; }
.cell.is-corr { background: var(--warn-soft); }
.cell-row { display: flex; align-items: center; gap: 4px; justify-content: flex-end; }
/* Ширина под «1 123 773,51» с разделителями тысяч — формат NumberField. */
.cell-input { width: 136px; text-align: right; font-family: var(--font-mono); font-variant-numeric: tabular-nums; border: 1px solid var(--border); border-radius: var(--rd-3); padding: 3px 7px; background: var(--bg-surface); }
.cell-input:focus { border-color: var(--accent); outline: none; }
.cell-input:disabled { background: var(--bg-tonal); color: var(--text-secondary); }
.cell-input.pct { width: 80px; }
.corr-btn { border: 1px solid var(--border); background: var(--bg-surface); border-radius: var(--rd-3); padding: 3px; cursor: pointer; color: var(--text-muted); display: inline-flex; }
.corr-btn.on { background: var(--warn-soft); color: var(--warn); border-color: var(--warn); }
.corr-reason { width: 100%; margin-top: 4px; font-size: var(--fs-2xs); border: 1px solid var(--warn); border-radius: var(--rd-3); padding: 3px 6px; background: var(--bg-surface); text-align: left; }

.calc-val { font-family: var(--font-mono); font-variant-numeric: tabular-nums; font-size: var(--fs-sm); }
.calc-val.neg { color: var(--neg-strong); }
.k-calc .calc-val { color: var(--text-secondary); }
.hint-val, .fact, .prev, .strat, .target, .share { display: block; font-size: var(--fs-2xs); font-family: var(--font-mono); margin-top: 2px; }
.hint-val { color: var(--warn); }
.fact { color: var(--text-muted); }
.prev { color: var(--text-muted); }
.strat { color: var(--accent); }
.target { color: var(--pos-strong); }
.share { color: var(--text-muted); }
.pct-sign { font-size: var(--fs-2xs); color: var(--text-muted); }
.cur-switch .select-sm { height: 30px; }

.mp-filters { display: flex; flex-wrap: wrap; align-items: flex-end; gap: var(--sp-4); margin-bottom: var(--sp-4); }
.mf { display: flex; flex-direction: column; gap: 2px; }
.mf-lbl { font-size: var(--fs-2xs); color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.03em; }
.mf .select-sm { height: 30px; }
.chip-row { display: flex; gap: 4px; }
.chip { border: 1px solid var(--border); background: var(--bg-surface); color: var(--text-secondary); border-radius: 999px; padding: 3px 12px; font-size: var(--fs-2xs); cursor: pointer; }
.chip.active { background: var(--accent-soft); color: var(--accent); border-color: var(--accent); }
.grp-row .grp-head { text-align: center; font-size: var(--fs-2xs); text-transform: uppercase; letter-spacing: .04em; color: var(--text-secondary); background: var(--bg-tonal); border-bottom: 1px solid var(--border); }
.muted { color: var(--text-muted); }

.total-cell { text-align: right; background: var(--bg-tonal); font-weight: var(--fw-medium); }

/* ── режим расчёта, вход в площадку, итоги формы, проверка ── */
.mode-tag { color: var(--text-muted); }
.mode-tag.inv { color: var(--accent); font-weight: var(--fw-semibold); }
.plat-btn {
  border: 0; background: none; padding: 0; cursor: pointer;
  font: inherit; color: var(--text-strong);
  display: inline-flex; align-items: center; gap: 4px;
}
.plat-btn:hover { color: var(--accent); }
.plat-ic { width: 12px; height: 12px; color: var(--text-muted); }
.plat-btn:hover .plat-ic { color: var(--accent); }
.calc-hint { display: block; font-size: var(--fs-2xs); font-family: var(--font-mono); color: var(--accent); margin-top: 2px; }
.tot-row td { border-top: 1px solid var(--border); padding: var(--sp-2) var(--sp-4); background: var(--bg-surface-2); }
.tot-row .col-line { font-weight: var(--fw-semibold); }
.tot-note { text-align: left; font-size: var(--fs-2xs); color: var(--text-muted); }
.tot-warn { color: var(--warn); }
.tot-row .total-cell { font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
.strong { font-weight: var(--fw-semibold); }
.check-card { padding: 0; overflow: hidden; }
.check-card .th-code { width: 90px; }
.check-ok { padding: var(--sp-4); font-size: var(--fs-sm); color: var(--pos-strong); margin: 0; }
.check-why { padding: 0 var(--sp-4) var(--sp-4); font-size: var(--fs-2xs); color: var(--warn); margin: 0; }
.row-block td { background: var(--neg-soft); }
.row-warn td { background: var(--warn-soft); }
.need-note { display: block; font-size: var(--fs-2xs); color: var(--neg); }
.cfo-inline { margin-left: 6px; font-family: var(--font-mono); font-size: var(--fs-2xs); color: var(--text-muted); }
.link { color: var(--accent); }
.dirty-dot { color: var(--warn); margin-left: 4px; font-size: 10px; vertical-align: middle; }
.empty-cell { text-align: center; color: var(--text-muted); padding: var(--sp-6); }
.hidden-file { display: none; }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
