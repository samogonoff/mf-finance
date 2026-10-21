<template>
  <div class="page-pl-card">
    <header class="page-header">
      <div>
        <h1 class="page-title">Тактический план · {{ year }}-{{ String(month).padStart(2, "0") }}</h1>
        <p class="page-subtitle">Формирование тактических планов Profit &amp; Loss — согласование по схеме</p>
      </div>
      <div class="page-actions">
        <NuxtLink :to="`/plans/route-scheme/${id}`" class="btn btn-ghost">
          <Icon name="lucide:git-branch" /> Схема согласования
        </NuxtLink>
        <!-- Своды — одним меню: пять кнопок в ряд на узком экране выталкивали «Процесс · задания» за край. -->
        <div v-if="svodLinks.length" ref="svodMenuEl" class="svod-menu">
          <button
            type="button"
            class="btn btn-ghost"
            aria-haspopup="menu"
            :aria-expanded="svodOpen"
            @click="svodOpen = !svodOpen"
          >
            <Icon name="lucide:layers" /> Своды
            <Icon :name="svodOpen ? 'lucide:chevron-up' : 'lucide:chevron-down'" />
          </button>
          <div v-if="svodOpen" class="svod-menu-panel" role="menu">
            <NuxtLink
              v-for="l in svodLinks"
              :key="l.to"
              :to="l.to"
              class="svod-menu-item"
              role="menuitem"
              @click="svodOpen = false"
            >
              <Icon :name="l.icon" /> {{ l.title }}
            </NuxtLink>
          </div>
        </div>
        <NuxtLink :to="`/plans/process/${id}`" class="btn btn-primary">
          <Icon name="lucide:list-checks" /> Процесс · задания
        </NuxtLink>
      </div>
    </header>

    <PlanStatusBar :pl-id="id" :year="year" :month="month" />

    <div class="segmented" role="tablist">
      <button class="seg" :class="{ active: tab === 'process' }" @click="tab = 'process'">Процесс</button>
      <button class="seg" :class="{ active: tab === 'pnl' }" @click="tab = 'pnl'">Свод · все данные</button>
      <button class="seg" :class="{ active: tab === 'svod' }" @click="tab = 'svod'">Свод по ЮЛ</button>
    </div>

    <p v-if="error" class="banner banner-neg">{{ error }}</p>

    <!-- ===== ПРОЦЕСС: календарный таймлайн ===== -->
    <section v-show="tab === 'process'" class="timeline card">
      <div class="tl-scroll">
        <div class="tl-grid" :style="{ gridTemplateColumns: `var(--tl-track) repeat(${COLS.length}, minmax(var(--tl-col-min), 1fr))` }">
          <!-- шапка календаря -->
          <div class="tl-corner">Календарь</div>
          <div v-for="c in COLS" :key="c.key" class="tl-colhead">
            <span class="tl-col-label">{{ c.label }}</span>
            <span class="tl-col-sub">{{ c.sub }}</span>
            <span v-if="colDue(c)" class="tl-col-date">{{ colDue(c) }}</span>
          </div>

          <!-- треки -->
          <template v-for="tr in TRACKS" :key="tr.key">
            <div class="tl-track" :class="`track-${tr.key}`">{{ tr.label }}</div>
            <div
              v-for="c in COLS"
              :key="tr.key + c.key"
              class="tl-cell"
            >
              <article
                v-for="st in cellStages(tr, c)"
                :key="st.stage_code"
                class="stage-card"
                :class="`s-${st.status}`"
              >
                <div class="sc-top">
                  <span class="sc-code">{{ st.route_only ? "Шаг маршрута" : `Этап ${st.stage_code}` }}</span>
                  <span class="badge" :class="statusBadge(st.status)">{{ statusLabel(st.status) }}</span>
                </div>
                <div class="sc-name">{{ st.name }}</div>
                <button
                  type="button"
                  class="sc-tasks"
                  :title="stageIsApproval(st) ? `Что согласуется на этапе ${st.stage_code}` : `Открыть задания этапа ${st.stage_code}`"
                  @click="stageModal = st"
                >
                  <!-- На этапе согласования заданий не бывает: показываем не «0/0
                       заданий» (это читалось как «ничего не сгенерировано»), а
                       сколько форм ждёт решения на этом шаге. -->
                  <span
                    v-if="stageIsApproval(st) && !stageTasks(st.stage_code).length"
                    class="sct-count"
                    :class="{ ok: !cardsOnStep(st.stage_code).length }"
                  >
                    <Icon name="lucide:check-check" />
                    {{ waitingLabel(st.stage_code) }}
                  </span>
                  <span v-else class="sct-count" :class="{ ok: stageTasks(st.stage_code).length && stageDone(st.stage_code) === stageTasks(st.stage_code).length }">
                    <Icon name="lucide:list-checks" /> {{ stageDone(st.stage_code) }}/{{ stageTasks(st.stage_code).length }} заданий
                  </span>
                  <span class="sct-people" :title="stageAssignees(st.stage_code).join(', ')">
                    <Icon name="lucide:users" /> {{ peopleLabel(st.stage_code) }}
                  </span>
                </button>
                <div class="sc-meta">
                  <span v-if="depHint(st)" class="sc-dep"><Icon name="lucide:lock" /> {{ depHint(st) }}</span>
                  <span v-else-if="st.stage_code === '1.6' && st.status !== 'completed' && leOpenForms.length" class="sc-dep"
                    :title="leOpenForms.join('\n')">
                    <Icon name="lucide:lock" /> ждёт утверждения форм: {{ leOpenForms.length }} (отправьте на согласование в «Согласование» у формы)
                  </span>
                  <span v-else-if="st.due_at" class="sc-due"><Icon name="lucide:calendar" /> {{ st.due_at }}</span>
                </div>
                <!-- У шага маршрута нет состояния в БД: двигать его нельзя,
                     сервер такой переход отклонит. Показываем только, у кого
                     формы, и ведём в лист согласования. -->
                <div v-if="st.route_only" class="sc-actions">
                  <button
                    class="btn btn-sm btn-ghost"
                    :title="`Формы, ожидающие шага «${st.name}»`"
                    @click="stageModal = st"
                  >
                    <Icon name="lucide:check-check" />
                    {{ cardsOnStep(st.stage_code).length ? "К формам на решении" : "Что согласуется" }}
                  </button>
                </div>
                <div v-else class="sc-actions">
                  <!-- Этапы 1.1–1.4 закрываются САМИ, когда формы проходят
                       соответствующий шаг: они агрегат состояния карточек, а не
                       отдельное решение. Кнопку здесь не показываем — сервер её
                       всё равно отклонит, а человек считал, что двигает процесс
                       (именно так процесс и «разъезжался» с формами). Вместо
                       кнопки — переход туда, где решение принимается. -->
                  <button
                    v-if="stageDrivenByCards(st)"
                    class="btn btn-sm btn-ghost"
                    :title="`Этап ${st.stage_code} закрывается сам, когда формы пройдут этот шаг`"
                    @click="stageModal = st"
                  >
                    <Icon name="lucide:check-check" />
                    {{ cardsOnStep(st.stage_code).length ? "К формам на решении" : "Что согласуется" }}
                  </button>
                  <button
                    v-else-if="!isApproval(st.stage_code)"
                    class="btn btn-sm btn-primary"
                    :disabled="!!depHint(st) || st.status === 'completed'"
                    @click="doAct(st.stage_code, 'submit')"
                  >Отправить</button>
                  <!-- Этапы 3 и 4 завершаются решениями согласующих на экране «Бюджет по ЮЛ» (ручное «Согласовать» сервер отклонит). -->
                  <NuxtLink
                    v-else-if="st.stage_code === '3' || st.stage_code === '4'"
                    :to="`/plans/le-svod/${id}`"
                    class="btn btn-sm btn-primary"
                    :class="{ disabled: !!depHint(st) }"
                  >К согласованию</NuxtLink>
                  <button
                    v-else
                    class="btn btn-sm btn-primary"
                    :disabled="!!depHint(st) || st.status === 'completed'"
                    @click="doAct(st.stage_code, 'approve')"
                  >Согласовать</button>
                  <button
                    v-if="canReturn(st)"
                    class="btn btn-sm btn-ghost"
                    @click="openReturn(st.stage_code)"
                  >Вернуть</button>
                </div>
                <div v-if="stageForms(st.stage_code).length" class="sc-forms">
                  <NuxtLink
                    v-for="f in stageForms(st.stage_code)"
                    :key="f.to"
                    :to="f.to"
                    class="sc-formlink"
                  ><Icon name="lucide:file-input" /> {{ f.label }}</NuxtLink>
                </div>
              </article>
            </div>
          </template>
        </div>
      </div>

      <!-- ===== Формы периода (карточки процесса) =====
           Таймлайн этапов показывает КАЛЕНДАРЬ периода, а карточки — состояние
           каждой формы отдельно: по ТЗ §2.2 единица маршрута это форма, поэтому
           возврат по мелким МП не должен выглядеть как возврат всего периода. -->
      <div class="cards-block">
        <div class="cb-head">
          <h3 class="cb-title">Формы периода</h3>
          <span class="cb-hint">
            У каждой формы свой маршрут, статус и версия. Клик по «Согласование» открывает
            лист согласования и публикацию.
          </span>
        </div>
        <p v-if="cardsError" class="banner banner-warn">{{ cardsError }}</p>
        <p v-else-if="!cards.length" class="cb-empty">Карточки форм на период не созданы.</p>

        <div v-for="c in cards" :key="c.id" class="fcard">
          <div class="fc-main">
            <div class="fc-id">
              <span class="fc-title">{{ c.title }}</span>
              <span class="fc-meta">
                <span class="fc-form">{{ c.form_code }}</span>
                <span v-if="c.scope_key">· {{ scopeLabel(c.scope_key) }}</span>
                <span v-if="c.legal_entity">· {{ c.legal_entity }}</span>
                · версия {{ c.current_version }}
                · {{ c.calc_mode === "inverse" ? "расчёт от условий" : "расчёт от сумм" }}
              </span>
            </div>
            <span class="badge" :class="cardBadge(c)">{{ cardStatus(c) }}</span>
            <span v-if="c.locked" class="fc-lock"><Icon name="lucide:lock" /> закрыт</span>
            <span v-if="c.form_code === 'TPL-TO-RETAIL' && c.missing_countries?.length" class="fc-lock">
              Сипарова утвердит, когда страны дойдут до 1.4. Ждём: {{ c.missing_countries.join(', ') }}
            </span>
          </div>
          <div class="fc-links">
            <!-- /plans/mp-form — экран МП, а не общий: задание по рознице открывать
                 им нельзя (у розницы своя форма ниже). Без проверки form_code
                 карточка розницы получала вторую ссылку «Форма», ведущую на форму МП. -->
            <!-- Разрез карточки идёт в ссылку: крупные и мелкие МП — разные
                 карточки с разными согласующими, и открывать они должны разные
                 формы (замечание аналитика 08.09.2026). -->
            <NuxtLink v-if="c.form_code === 'TPL-MP' && formTaskId(c)" :to="`/plans/mp-form/${formTaskId(c)}?cardId=${c.id}&scope=${c.scope_key}`" class="fc-link">
              <Icon name="lucide:file-input" /> Форма
            </NuxtLink>
            <NuxtLink v-if="c.form_code === 'TPL-MP'" :to="`/plans/mp-conditions/${c.id}`" class="fc-link">
              <Icon name="lucide:sliders-horizontal" /> Условия
            </NuxtLink>
            <NuxtLink v-if="c.form_code === 'TPL-MP'" :to="`/plans/mp-common-costs/${c.id}`" class="fc-link">
              <Icon name="lucide:layers" /> Общие затраты
            </NuxtLink>
            <!-- Розница: форма ввода и экран согласования — отдельные экраны, как
                 и требует ТЗ (§4 ввод / §5 свод); заданий (pl_task) у неё нет,
                 поэтому ссылка идёт на карточку, а не на задание. -->
            <NuxtLink v-if="c.form_code === 'TPL-TO-RETAIL'" :to="`/plans/retail-form/${c.id}`" class="fc-link">
              <Icon name="lucide:file-input" /> Форма
            </NuxtLink>
            <NuxtLink v-if="reviewUrl(c) && c.status === 'on_approval'" :to="reviewUrl(c)" class="fc-link">
              <Icon name="lucide:eye" /> Проверить
            </NuxtLink>
            <NuxtLink v-if="c.form_code === 'TPL-TO-RETAIL'" :to="`/plans/retail-summary/${c.id}`" class="fc-link">
              <Icon name="lucide:layout-list" /> Свод
            </NuxtLink>
			<!-- Legacy 1.5 cards keep the shared grid; CFO v2 cards use the
			     code-based annual form. -->
			<NuxtLink v-if="c.form_code === 'TPL-CFO-EXP' && !c.scope_key?.startsWith('CFO-')" :to="`/plans/exp-form/${c.id}`" class="fc-link">
              <Icon name="lucide:file-input" /> Форма
            </NuxtLink>
			<NuxtLink v-if="c.form_code === 'TPL-CFO-EXP' && c.scope_key?.startsWith('CFO-')" :to="`/plans/cfo-exp/${c.id}`" class="fc-link">
			  <Icon name="lucide:file-input" /> Форма ЦФО
			</NuxtLink>
            <button type="button" class="fc-link btn-like" @click="openCard = openCard === c.id ? 0 : c.id">
              <Icon name="lucide:check-check" /> Согласование
            </button>
          </div>
          <CardProcessPanel v-if="openCard === c.id" :card-id="c.id" :review-url="reviewUrl(c)"
            :can-submit="c.form_code !== 'TPL-TO-RETAIL'"
            submit-hint="Откройте форму розницы и подтвердите итоги по четырём статусам перед отправкой"
            @changed="loadCards" />
        </div>
      </div>

      <div class="tl-legend">
        <span><i class="dot s-pending"></i> ожидает</span>
        <span><i class="dot s-in_progress"></i> в работе</span>
        <span><i class="dot s-completed"></i> завершён</span>
        <span><i class="dot s-returned"></i> возвращён</span>
      </div>
    </section>

    <!-- ===== СВОД ПЕРИОДА: все данные карточки в одном окне ===== -->
    <section v-show="tab === 'pnl'" class="card">
      <div class="card-header pnl-head">
        <span class="card-title">Свод периода — все данные карточки (read-only)</span>
        <div class="pnl-actions">
          <button v-if="canAdmin" class="btn btn-sm btn-ghost" @click="stratInput?.click()"><Icon name="lucide:upload" /> Импорт стратегии</button>
          <input ref="stratInput" type="file" accept=".xlsx" class="hidden-file" @change="doStrategyImport" />
        </div>
      </div>
      <p v-if="pnlNote" class="banner banner-pos">{{ pnlNote }}</p>
      <MpBoard v-if="tab === 'pnl'" :key="boardKey" :pl-id="id" />
    </section>

    <!-- ===== СВОД ===== -->
    <section v-show="tab === 'svod'" class="card">
      <div class="card-header"><span class="card-title">Свод по ЮЛ × каналам (TPL-08, товарооборот)</span></div>
      <table class="data-table">
        <thead>
          <tr><th>Юридическое лицо</th><th>Канал</th><th class="col-num">Товарооборот</th><th>Валюта</th></tr>
        </thead>
        <tbody>
          <tr v-if="!svod.length"><td colspan="4" class="empty-cell">Нет данных тактики за период — заполните формы МП и розницы.</td></tr>
          <tr v-for="(s, i) in svod" :key="i">
            <td class="le-cell">{{ s.legal_entity }}</td>
            <td>{{ s.channel }}</td>
            <td class="col-num strong">{{ money(s.amount, { currency: "" }) }}</td>
            <td>{{ s.currency }}</td>
          </tr>
        </tbody>
        <!-- Итог по валютам, а не одной строкой: маркетплейсы ведутся в RUB,
             розница — в нац. валюте страны, и складывать их без курса нельзя. -->
        <tfoot v-if="svod.length">
          <tr v-for="t in svodTotals" :key="t.currency" class="tot-row">
            <td class="le-cell">Итого</td>
            <td>{{ t.channels }}</td>
            <td class="col-num strong">{{ money(t.amount, { currency: "" }) }}</td>
            <td>{{ t.currency }}</td>
          </tr>
        </tfoot>
      </table>
      <p class="prov-note">
        Свод собирает весь комплект: маркетплейсы и розницу по странам. Маппинг площадка → ЮЛ
        провизорный (на уточнении у аналитика).
      </p>
    </section>

    <!-- ===== Модалка возврата ===== -->
    <PlansModal
      :open="ret.open"
      :title="`Вернуть этап ${ret.from}`"
      subtitle="Выберите этап для доработки и укажите причину"
      @close="ret.open = false"
    >
      <label class="field">
        <span class="field-label">Вернуть на этап</span>
        <select v-model="ret.target" class="select">
          <option value="" disabled>— выберите —</option>
          <option v-for="o in returnTargets(ret.from)" :key="o.code" :value="o.code">
            {{ o.code }} — {{ o.name }}
          </option>
        </select>
      </label>
      <label class="field">
        <span class="field-label">Причина возврата <span class="req">*</span></span>
        <textarea v-model="ret.reason" class="select textarea" rows="3" placeholder="что доработать"></textarea>
        <span class="field-hint">Обязательно: возврат без комментария не принимается</span>
      </label>
      <template #footer>
        <button class="btn btn-ghost" @click="ret.open = false">Отмена</button>
        <button class="btn btn-danger" :disabled="!ret.target || !ret.reason.trim() || busy" @click="submitReturn">
          <Icon name="lucide:corner-up-left" /> Вернуть
        </button>
      </template>
    </PlansModal>

    <!-- ===== Попап этапа: полная информация ===== -->
    <PlansModal
      :open="!!stageModal"
      :title="stageModal ? `Этап ${stageModal.stage_code} · ${stageModal.name}` : ''"
      :subtitle="stageModal ? `${stageDone(stageModal.stage_code)} из ${stageTasks(stageModal.stage_code).length} заданий выполнено · статус: ${statusLabel(stageModal.status)}` : ''"
      @close="stageModal = null"
    >
      <div v-if="stageModal" class="sm-body">
        <!-- Этап согласования заданий-ввода не имеет: решение принимается по
             КАРТОЧКАМ ФОРМ (у каждой формы свой лист согласования, ТЗ §2.2).
             Раньше здесь предлагалось «сгенерировать задания», и человек искал
             кнопку, которой не существует. -->
        <div v-if="!stageTasks(stageModal.stage_code).length && stageIsApproval(stageModal)" class="sm-appr">
          <p class="sm-appr-txt">
            Это этап согласования — отдельных заданий у него нет. Согласуются
            <b>карточки форм</b>: у каждой формы свой лист согласования, версия и возврат,
            поэтому возврат одной формы не останавливает остальные.
          </p>
          <p v-if="!cardsOnStep(stageModal.stage_code).length" class="sm-appr-txt muted">
            На этом шаге сейчас ни одна форма решения не ждёт.
          </p>
          <div v-else class="sm-cards">
            <div v-for="c in cardsOnStep(stageModal.stage_code)" :key="c.id" class="sm-card">
              <span class="smc-title">{{ c.title }}</span>
              <span class="badge" :class="cardBadge(c)">{{ cardStatus(c) }}</span>
              <!-- Лист согласования — панель на этом же экране (CardProcessPanel),
                   поэтому не ссылка, а раскрытие панели: отдельной страницы
                   согласования карточки нет. -->
              <NuxtLink v-if="reviewUrl(c)" :to="reviewUrl(c)" class="btn btn-sm btn-primary" @click="stageModal = null">
                <Icon name="lucide:eye" /> Проверить
              </NuxtLink>
              <button type="button" class="btn btn-sm btn-ghost" @click="openCardFromStage(c.id)">
                <Icon name="lucide:check-check" /> Согласование
              </button>
            </div>
          </div>
        </div>
        <p v-else-if="!stageTasks(stageModal.stage_code).length && stageModal.has_task_templates === false" class="sm-empty">
          На этот этап не заведены шаблоны заданий — генерация ничего не создаст.
          Шаблоны настраиваются в
          <NuxtLink :to="`/plans/admin/task-templates`" class="link">конструкторе заданий</NuxtLink>.
        </p>
        <p v-else-if="!stageTasks(stageModal.stage_code).length" class="sm-empty">
          Задания не сгенерированы.
          <NuxtLink :to="`/plans/process/${id}`" class="link">Открыть кокпит</NuxtLink> и нажать «Генерировать задания».
        </p>
        <div v-else class="sm-tasks">
          <div v-for="t in stageTasks(stageModal.stage_code)" :key="t.id" class="sm-task" :class="`st-${t.status}`">
            <div class="smt-head">
              <span class="badge" :class="t.task_role === 'approve' ? 'badge-accent' : 'badge-info'">{{ t.task_role === "approve" ? "согл." : "ввод" }}</span>
              <b class="smt-title">{{ t.title }}</b>
              <span class="badge badge-dot" :class="taskStTone(t.status)">{{ taskStLabel(t.status) }}</span>
            </div>
            <div class="smt-meta">
              <span class="smt-asg"><Icon name="lucide:user-round" /> отв.: {{ t.assignee_name || "не назначен" }}</span>
              <span v-if="t.delegate_name" class="smt-del"><Icon name="lucide:share" /> делегат: {{ t.delegate_name }}</span>
              <span v-if="filledBy(t)" class="smt-filled"><Icon name="lucide:check" /> заполнил: {{ filledBy(t) }}</span>
              <span v-if="t.cfo_count" class="smt-cfo">{{ t.cfo_count }} ЦФО</span>
              <span class="smt-form">{{ t.form_code }}</span>
            </div>
            <div class="smt-act">
              <NuxtLink v-if="t.form_code === 'TPL-MP'" :to="`/plans/mp-form/${t.id}`" class="btn btn-sm btn-ghost"><Icon name="lucide:pencil" /> Форма</NuxtLink>
            </div>
          </div>
        </div>
      </div>
      <template #footer>
        <NuxtLink :to="`/plans/process/${id}`" class="btn btn-primary"><Icon name="lucide:list-checks" /> В кокпит процесса</NuxtLink>
      </template>
    </PlansModal>
  </div>
</template>

<script setup lang="ts">
import { apiError } from "~/utils/apiError";
import { cardStatusText, cardStatusTone } from "~/utils/cardStatus";
import { money } from "~/utils/format";
import PlansModal from "~/components/plans/PlansModal.vue";
import MpBoard from "~/components/plans/MpBoard.vue";
import PlanStatusBar from "~/components/plans/PlanStatusBar.vue";
import CardProcessPanel from "~/components/plans/CardProcessPanel.vue";
import { usePlans, type StageState, type SvodRow } from "~/composables/usePlans";
import { useTasks, type Task } from "~/composables/useTasks";
import { useMpConditions, type PlanCard, type RouteStep } from "~/composables/useMpConditions";

definePageMeta({ middleware: "scope-guard" });

const route = useRoute();
const id = Number(route.params.id);
const year = ref(Number(route.query.year) || 2026);
const month = ref(Number(route.query.month) || 6);

const { stages, stageAction, mpSvod, addComment } = usePlans();
const { byInstance, importStrategy } = useTasks();
const { hasRole, isAdmin } = useScope();
const canAdmin = computed(() => isAdmin.value || hasRole("ROLE_PLANS_ADMIN"));

const pnlNote = ref("");
const stratInput = ref<HTMLInputElement | null>(null);
// Смена ключа перемонтирует свод — так он перечитает данные после импорта стратегии.
const boardKey = ref(0);
const doStrategyImport = async (ev: Event) => {
  const f = (ev.target as HTMLInputElement).files?.[0];
  if (!f) return;
  pnlNote.value = ""; error.value = "";
  try {
    const r = await importStrategy(id, f);
    pnlNote.value = `Импортировано стратегии: ${r.imported} строк`;
    boardKey.value++;
  } catch (e) { error.value = apiError(e, "Ошибка импорта стратегии"); }
  finally { if (stratInput.value) stratInput.value.value = ""; }
};

const list = ref<StageState[]>([]);
const tasks = ref<Task[]>([]);
const cards = ref<PlanCard[]>([]);
const cardsError = ref("");
const openCard = ref(0);
const cardApi = useMpConditions();
const svod = ref<SvodRow[]>([]);
// Итоги свода — по валютам: суммы разных валют не складываются, а сколько
// каналов вошло в каждую, видно сразу.
const svodTotals = computed(() => {
  const by = new Map<string, { currency: string; amount: number; set: Set<string> }>();
  for (const r of svod.value) {
    const cur = r.currency || "—";
    const t = by.get(cur) || { currency: cur, amount: 0, set: new Set<string>() };
    t.amount += r.amount;
    t.set.add(r.channel);
    by.set(cur, t);
  }
  return [...by.values()]
    .map((t) => ({ currency: t.currency, amount: t.amount, channels: [...t.set].sort().join(", ") }))
    .sort((a, b) => a.currency.localeCompare(b.currency));
});
const error = ref("");
const busy = ref(false);
const tab = ref<"process" | "pnl" | "svod">("process");
const ret = reactive({ open: false, from: "", target: "", reason: "" });
const stageModal = ref<StageState | null>(null);

// Подписи состояний — только из общего словаря (usePlanStatus), чтобы этапы и
// задания на всех экранах назывались одинаково.
const planStatus = usePlanStatus();
const taskStLabel = (s: string) => planStatus.label(s);
const taskStTone = (s: string) => planStatus.badge(s);
const stageTasks = (code: string) => tasks.value.filter((t) => t.stage_code === code);
const stageAssignees = (code: string) => [...new Set(stageTasks(code).map((t) => t.assignee_name).filter(Boolean))];
const stageDone = (code: string) => stageTasks(code).filter((t) => t.status === "done").length;
const peopleLabel = (code: string) => {
  const a = stageAssignees(code);
  if (!a.length) return "исполнители не назначены";
  return a.slice(0, 3).join(", ") + (a.length > 3 ? ` +${a.length - 3}` : "");
};
// Кто реально заполнил: делегат (если делегировано) или исполнитель — для сдано/проверки/готово.
const filledBy = (t: Task) => (["review", "done"].includes(t.status) ? (t.delegate_name || t.assignee_name) : "");

const COLS = [
  { key: "prev25", label: "25-е число", sub: "месяц М−1", codes: ["2.1", "2.2"] },
  { key: "rd2", label: "2-й р.д.", sub: "заполнение", codes: ["1.1"] },
  { key: "rd3", label: "3-й р.д.", sub: "согласование", codes: ["1.2", "1.3", "2.3"] },
  // «финал/свод» на 4-м р.д. спорил с «утверждением» на 6-м: на таймлайне
  // получались два финала. 1.4 — утверждение потока продаж, 4 — итоговое.
  { key: "rd4", label: "4-й р.д.", sub: "утверждение продаж / свод", codes: ["1.4", "2.4"] },
  { key: "rd5", label: "5-й р.д.", sub: "расходы/ЮЛ", codes: ["1.5", "1.6"] },
  { key: "rd6", label: "6-й р.д.", sub: "утверждение", codes: ["3", "4"] }
];
const TRACKS = [
  { key: "sales", label: "Поток продаж", codes: ["1.1", "1.2", "1.3", "1.4", "1.5", "1.6"] },
  { key: "production", label: "Поток производства", codes: ["2.1", "2.2", "2.3", "2.4"] },
  { key: "final", label: "Финал — ЮЛ", codes: ["3", "4"] }
];
const APPROVAL = new Set(["1.2", "1.3", "1.4", "2.2", "3", "4"]);

const byCode = computed<Record<string, StageState>>(() =>
  Object.fromEntries(list.value.map((s) => [s.stage_code, s]))
);

type Col = (typeof COLS)[number];
type Track = (typeof TRACKS)[number];

const cellStages = (tr: Track, c: Col): StageState[] => {
  const own = tr.codes
    .filter((code) => c.codes.includes(code) && byCode.value[code])
    .map((code) => byCode.value[code]);
  // Шаги маршрута форм, которых нет среди этапов периода, показываем рядом с
  // согласованием: иначе на таймлайне не видно, у кого форма.
  if (tr.key === "sales" && c.key === "rd3") return [...own, ...routeOnlySteps.value];
  return own;
};

/** Шаги маршрута, включённые данными и не имеющие своего этапа периода. */
const routeOnlySteps = computed<StageState[]>(() => {
  const seen = new Set<string>();
  const out: StageState[] = [];
  for (const steps of Object.values(routes.value)) {
    for (const st of steps) {
      // Этап периода 1.2 у форм продаж включает и шаг контролёра fin (stage_sync.go).
      if (!st.enabled || st.step_code === "fin" && ["TPL-MP", "TPL-TO-RETAIL"].includes(st.form_code) ||
          byCode.value[st.step_code] || seen.has(st.step_code)) continue;
      seen.add(st.step_code);
      const waiting = cardsOnStep(st.step_code).length;
      out.push({
        stage_code: st.step_code,
        track: "sales",
        name: st.step_name,
        // Статус синтетический: этапа в БД нет, состояние определяют формы.
        status: waiting ? "in_progress" : "pending",
        due_at: "",
        depends_on: [],
        responsible: st.responsible,
        is_approval: true,
        has_task_templates: false,
        route_only: true
      });
    }
  }
  return out;
});

const colDue = (c: Col): string => {
  for (const code of c.codes) {
    const s = byCode.value[code];
    if (s?.due_at) return s.due_at;
  }
  return "";
};

const isApproval = (code: string) => APPROVAL.has(code);

/**
 * Этап согласования: признак приходит с сервера (stageDefs.IsApproval), а
 * локальный список APPROVAL остаётся фолбэком для старых ответов API.
 */
const stageIsApproval = (st: StageState) => st.is_approval ?? APPROVAL.has(st.stage_code);

/**
 * Карточки форм, ожидающие решения на этом шаге маршрута. Шаг карточки (step_code)
 * совпадает с кодом этапа: 1.2 — руководитель, 1.3 — директор, 1.4 — финальное
 * утверждение. Показываем только те, что реально на согласовании: карточка в
 * черновике решения не ждёт.
 */
/**
 * Этапом управляют КАРТОЧКИ форм: 1.1–1.4 закрываются сами, когда формы проходят
 * шаг (сервер отклоняет ручное закрытие). Признак считаем по наличию карточек с
 * таким шагом в маршруте — если карточек периода нет вовсе, этап остаётся
 * ручным, как этапы производства и сводов.
 */
const CARD_STEPS = ["1.1", "1.2", "1.3", "1.4"];
const stageDrivenByCards = (st: StageState) => CARD_STEPS.includes(st.stage_code) && cards.value.length > 0;

/**
 * Плашка ожидания: «1 форма ждёт решения», «2 формы», «5 форм». Раньше стояло
 * «1 форм ждёт решения» — число и слово не согласовывались (замечание аналитика
 * 08.09.2026).
 */
const waitingLabel = (code: string): string => {
  const n = cardsOnStep(code).length;
  if (!n) return "формы решения не ждут";
  const last = n % 10;
  const tens = n % 100;
  const word = tens >= 11 && tens <= 14 ? "форм" : last === 1 ? "форма" : last >= 2 && last <= 4 ? "формы" : "форм";
  const verb = tens >= 11 && tens <= 14 ? "ждут" : last === 1 ? "ждёт" : "ждут";
  return `${n} ${word} ${verb} решения`;
};

const cardsOnStep = (code: string) =>
  cards.value.filter((c) => c.form_code !== "TPL-CFO-EXP" &&
    (c.step_code === code || code === "1.2" && c.step_code === "fin") &&
    ["on_approval", "returned"].includes(c.status));

/** Из попапа этапа — к листу согласования карточки: закрываем попап и раскрываем панель. */
const openCardFromStage = (cardID: number) => {
  stageModal.value = null;
  openCard.value = cardID;
  // Панель рендерится ниже таймлайна — без прокрутки человек её не заметит и
  // решит, что кнопка «не сработала».
  nextTick(() => {
    document.querySelector(".cards-block")?.scrollIntoView({ behavior: "smooth", block: "start" });
  });
};
const canReturn = (st: StageState) => st.status !== "pending";

const depHint = (st: StageState): string => {
  const unmet = (st.depends_on || []).filter((d) => byCode.value[d]?.status !== "completed");
  return unmet.length ? "ждёт " + unmet.join(", ") : "";
};

// Формы продаж и расходов периода, которые ещё не утверждены: они держат этап 1.6 (сервер проверяет то же правило).
const leOpenForms = computed(() =>
  cards.value
    .filter((c) =>
      (["TPL-TO-RETAIL", "TPL-MP"].includes(c.form_code) || (c.form_code === "TPL-CFO-EXP" && !c.scope_key?.startsWith("CFO-"))) &&
      !["approved", "published", "archived"].includes(c.status))
    .map((c) => `${c.title} — ${cardStatusText(c)}`));

const statusLabel = (s: string) => planStatus.label(s);
const statusBadge = (s: string) => planStatus.badge(s);

const returnTargets = (from: string) => list.value.filter((s) => s.stage_code !== from).map((s) => ({ code: s.stage_code, name: s.name }));

// Формы/задания этапа — теперь через движок процесса (task-driven), а не старую
// segment-форму. Кнопка ведёт в кокпит процесса с заданиями этапа.
const stageForms = (code: string): Array<{ label: string; to: string }> => {
  // Ссылка нужна на КАЖДОМ этапе ввода, а не только на 1.1: задания есть и у 1.5,
  // 2.1, 2.3, но попасть в них можно было только через общий экран процесса
  // (найдено 07.09.2026). Признак — наличие заданий у этапа, а не список кодов:
  // добавление шаблона на новый этап не должно требовать правки фронта.
  if (!isApproval(code) && stageTasks(code).length) {
    return [{ label: "Задания этапа", to: `/plans/process/${id}` }];
  }
  return [];
};

// Человеческие метки статуса карточки: сервер отдаёт status_label технической
// строкой (on_approval_1.2), в списке форм нужна фраза, а не код.
const cardStatus = (c: PlanCard): string => cardStatusText(c);
const cardBadge = (c: PlanCard): string => cardStatusTone(c.status);
const scopeLabel = (scope: string) =>
  scope === "large" ? "крупные МП" : scope === "small" ? "мелкие МП" : scope === "all" ? "все площадки" : scope;
// Ввод формы остаётся task-driven: карточка про процесс, задание — про права и
// срез ЦФО. Связываем их по коду формы.
const formTaskId = (c: PlanCard): number => tasks.value.find((t) => t.stage_code === "1.1" &&
  t.form_code === c.form_code && (t.covered_card_ids?.includes(c.id) || t.card_id === c.id))?.id || 0;
const reviewUrl = (c: PlanCard): string => {
  if (c.form_code === "TPL-TO-RETAIL") return `/plans/retail-form/${c.id}?readonly=1`;
  if (c.form_code === "TPL-MP" && formTaskId(c))
    return `/plans/mp-form/${formTaskId(c)}?cardId=${c.id}&scope=${encodeURIComponent(c.scope_key)}&readonly=1`;
  return "";
};

const loadCards = async () => {
  cardsError.value = "";
  try {
    cards.value = await cardApi.cards(id);
    await loadRoutes();
  } catch (e: unknown) {
    cards.value = [];
    cardsError.value = apiError(e, "Карточки форм недоступны");
  }
};

// Маршруты форм периода. Нужны таймлайну: в маршруте есть шаги, которых нет
// среди этапов периода — прежде всего «Финансовый контролёр» (шаг `fin`,
// включается данными). Пока его не было видно, форма, стоящая на финансисте,
// выглядела как «этап 1.2 ждёт 1.1», и понять, у кого форма, было нельзя.
const routes = ref<Record<string, RouteStep[]>>({});
const loadRoutes = async () => {
  const codes = [...new Set(cards.value.map((c) => c.form_code))];
  const loaded = await Promise.all(
    codes.map((code) => cardApi.formRoute(code).then((steps) => [code, steps] as const).catch(() => [code, []] as const))
  );
  routes.value = Object.fromEntries(loaded);
};

const load = async () => {
  error.value = "";
  try {
    list.value = await stages(id, year.value, month.value);
    // Карточки грузим ВМЕСТЕ с остальным, а не после: от них зависит вид плиток
    // этапов (какие закрываются сами), и при загрузке в конце таймлайн на миг
    // показывал кнопки, которых там быть не должно.
    await Promise.all([
      mpSvod(year.value, month.value).then((v) => (svod.value = v)),
      byInstance(id).catch(() => []).then((v) => (tasks.value = v)),
      loadCards()
    ]);
    const requestedCard = Number(route.query.card);
    if (Number.isInteger(requestedCard) && cards.value.some((c) => c.id === requestedCard)) {
      openCard.value = requestedCard;
    }
  } catch (e: unknown) {
    error.value = apiError(e, "Ошибка загрузки");
  }
};

const doAct = async (code: string, action: string) => {
  error.value = "";
  busy.value = true;
  try {
    list.value = await stageAction(id, code, { action, year: year.value, month: month.value });
  } catch (e: unknown) {
    error.value = apiError(e, "Ошибка перехода");
  } finally {
    busy.value = false;
  }
};

const openReturn = (code: string) => {
  ret.from = code;
  ret.target = "";
  ret.reason = "";
  ret.open = true;
};

const submitReturn = async () => {
  // Комментарий обязателен: сервер отклоняет возврат без него (ТЗ §2.3, V-06).
  if (!ret.target || !ret.reason.trim()) return;
  busy.value = true;
  error.value = "";
  try {
    list.value = await stageAction(id, ret.from, {
      action: "return",
      target: ret.target,
      comment: ret.reason.trim(),
      year: year.value,
      month: month.value
    });
    await addComment(id, `Возврат ${ret.from} → ${ret.target}: ${ret.reason}`, `stage:${ret.from}`);
    ret.open = false;
  } catch (e: unknown) {
    error.value = apiError(e, "Ошибка возврата");
  } finally {
    busy.value = false;
  }
};

onMounted(load);

// Меню «Своды»: пункты зависят от карточек периода; закрывается по клику снаружи и по Escape.
interface SvodLink { to: string; title: string; icon: string }
const svodLinks = computed<SvodLink[]>(() => {
  const exp = cards.value.filter((c) => c.form_code === "TPL-CFO-EXP");
  const has = (key: string) => exp.some((c) => c.scope_key === key);
  const out: SvodLink[] = [];
  if (cards.value.some((c) => c.form_code === "TPL-TO-RETAIL")) {
    out.push({ to: `/plans/retail-consolidated/${id}`, title: "Продажи розницы · 4 страны", icon: "lucide:globe" });
  }
  if (exp.some((c) => (c.scope_key || "").startsWith("R-"))) {
    out.push({ to: `/plans/exp-consolidated/${id}`, title: "Расходы розницы · 4 страны", icon: "lucide:wallet" });
  }
  if (has("IM-BY") || has("OPT")) {
    out.push({ to: `/plans/exp-consolidated/${id}?channel=all`, title: "Расходы · все каналы", icon: "lucide:layers" });
  }
  if (has("IM-BY")) {
    out.push({ to: `/plans/exp-consolidated/${id}?channel=im`, title: "Расходы интернет-магазина", icon: "lucide:wallet" });
  }
  if (has("OPT")) {
    out.push({ to: `/plans/exp-consolidated/${id}?channel=opt`, title: "Расходы опта", icon: "lucide:wallet" });
  }
  if (cards.value.length) {
    out.push({ to: `/plans/le-svod/${id}`, title: "Бюджет по ЮЛ", icon: "lucide:building-2" });
  }
  return out;
});
const svodOpen = ref(false);
const svodMenuEl = ref<HTMLElement | null>(null);
const closeSvodOnOutside = (e: MouseEvent) => {
  if (svodOpen.value && svodMenuEl.value && !svodMenuEl.value.contains(e.target as Node)) svodOpen.value = false;
};
const closeSvodOnEscape = (e: KeyboardEvent) => {
  if (e.key === "Escape") svodOpen.value = false;
};
onMounted(() => {
  document.addEventListener("click", closeSvodOnOutside);
  document.addEventListener("keydown", closeSvodOnEscape);
});
onBeforeUnmount(() => {
  document.removeEventListener("click", closeSvodOnOutside);
  document.removeEventListener("keydown", closeSvodOnEscape);
});
</script>

<style scoped>
/* Кнопки шапки переносятся на узком экране, а не уезжают за край. */
.page-pl-card .page-actions { flex-wrap: wrap; justify-content: flex-end; }
.svod-menu { position: relative; }
.svod-menu-panel {
  position: absolute;
  right: 0;
  top: calc(100% + var(--sp-2));
  z-index: var(--z-dropdown);
  min-width: 280px;
  display: flex;
  flex-direction: column;
  padding: var(--sp-2);
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--rd-4, 6px);
  box-shadow: var(--shadow-pop);
}
.svod-menu-item {
  display: flex;
  align-items: center;
  gap: var(--sp-4);
  padding: var(--sp-4) var(--sp-5);
  border-radius: var(--rd-3, 4px);
  color: var(--text-primary);
  text-decoration: none;
  white-space: nowrap;
}
.svod-menu-item:hover,
.svod-menu-item:focus-visible { background: var(--bg-surface-3); outline: none; }

/* Итоговые строки свода: тот же тон, что у шапки таблицы. */
.tot-row td { background: var(--bg-tonal); font-weight: var(--fw-medium); }

.segmented {
  display: inline-flex;
  gap: 2px;
  background: var(--bg-surface-3);
  border-radius: var(--rd-4, 6px);
  padding: 2px;
  margin-bottom: var(--sp-5);
}
.seg {
  border: 0;
  background: transparent;
  padding: 4px 14px;
  border-radius: var(--rd-3, 4px);
  font-size: var(--fs-sm);
  color: var(--text-secondary);
  cursor: pointer;
}
.seg.active {
  background: var(--bg-surface);
  color: var(--text-strong);
  box-shadow: var(--shadow-pop);
}
.banner {
  padding: var(--sp-4) var(--sp-5);
  border-radius: var(--rd-4, 6px);
  margin-bottom: var(--sp-5);
  font-size: var(--fs-sm);
}
.banner-neg {
  background: var(--neg-soft);
  color: var(--neg-strong);
}

/* ── таймлайн ── */
.timeline {
  padding: var(--sp-5);
  --tl-track: 150px;
  /* Минимум колонки этапа. 150px не хватало на кнопку «К формам на решении»:
     она вылезала за границу карточки. Колонка держит самую длинную кнопку
     целиком, а лишнюю ширину забирает горизонтальная прокрутка .tl-scroll. */
  --tl-col-min: 176px;
}
.tl-scroll {
  overflow-x: auto;
}
.tl-grid {
  display: grid;
  gap: var(--sp-3);
  min-width: 920px;
}
.tl-corner {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  align-self: end;
  padding-bottom: var(--sp-3);
}
.tl-colhead {
  display: flex;
  flex-direction: column;
  gap: 1px;
  padding: var(--sp-3) var(--sp-4);
  border-bottom: 2px solid var(--border-strong);
}
.tl-col-label {
  font-weight: var(--fw-semibold);
  color: var(--text-strong);
  font-size: var(--fs-sm);
}
.tl-col-sub {
  font-size: var(--fs-2xs);
  color: var(--text-muted);
  text-transform: uppercase;
}
.tl-col-date {
  font-family: var(--font-mono);
  font-size: var(--fs-2xs);
  color: var(--text-secondary);
  margin-top: 2px;
}
.tl-track {
  display: flex;
  align-items: center;
  font-weight: var(--fw-semibold);
  font-size: var(--fs-sm);
  color: var(--text-inverse);
  padding: 0 var(--sp-4);
  border-radius: var(--rd-4, 6px);
}
.track-sales {
  background: #312e81;
}
.track-production {
  background: #0b5cad;
}
.track-final {
  background: #6b3410;
}
.tl-cell {
  display: flex;
  flex-direction: column;
  gap: var(--sp-3);
  min-width: 0;
  background: var(--bg-surface-2);
  border-radius: var(--rd-4, 6px);
  padding: var(--sp-3);
  min-height: 56px;
}
.stage-card {
  min-width: 0;
  background: var(--bg-surface);
  border: 1px solid var(--border);
  border-left: 3px solid var(--border-strong);
  border-radius: var(--rd-4, 6px);
  padding: var(--sp-4);
  display: flex;
  flex-direction: column;
  gap: var(--sp-3);
}
.stage-card.s-completed {
  border-left-color: var(--pos);
}
.stage-card.s-in_progress {
  border-left-color: var(--accent);
}
.stage-card.s-returned {
  border-left-color: var(--neg);
}
.stage-card.s-blocked {
  border-left-color: var(--warn);
}
.sc-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--sp-3);
}
.sc-code {
  font-family: var(--font-mono);
  font-size: var(--fs-xs);
  font-weight: var(--fw-semibold);
  color: var(--text-secondary);
}
.sc-name {
  font-size: var(--fs-sm);
  color: var(--text-strong);
  line-height: var(--lh-tight);
  /* Длинные названия этапов («Согласование руководителем подразделения»)
     переносятся, а не выдавливают соседние элементы карточки. */
  overflow-wrap: anywhere;
  min-width: 0;
}
.sc-tasks {
  display: flex;
  flex-direction: column;
  gap: 2px;
  align-items: flex-start;
  border: 1px solid var(--border);
  border-radius: var(--rd-3);
  background: var(--bg-surface-2);
  padding: 4px var(--sp-3);
  cursor: pointer;
  width: 100%;
  text-align: left;
}
.sc-tasks:hover { border-color: var(--accent); }
.sct-count {
  font-size: var(--fs-2xs);
  font-family: var(--font-mono);
  color: var(--text-secondary);
  display: inline-flex;
  align-items: flex-start;
  gap: 4px;
  /* Текст плашки переносится по словам: в узкой колонке таймлайна он налезал
     на соседние строки карточки. */
  text-align: left;
  overflow-wrap: anywhere;
  min-width: 0;
}
.sct-count.ok { color: var(--pos-strong); }
.sct-people {
  font-size: var(--fs-2xs);
  color: var(--text-muted);
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* попап этапа */
.sm-body { display: flex; flex-direction: column; gap: var(--sp-3); }
.sm-empty { color: var(--text-muted); padding: var(--sp-4); text-align: center; }
/* Этап согласования: объяснение + карточки, ожидающие решения на этом шаге. */
.sm-appr { display: flex; flex-direction: column; gap: var(--sp-3); padding: var(--sp-2) 0; }
.sm-appr-txt { margin: 0; font-size: var(--fs-sm); color: var(--text-primary); }
.sm-appr-txt.muted { color: var(--text-muted); }
.sm-cards { display: flex; flex-direction: column; gap: var(--sp-2); }
.sm-card {
  display: flex;
  align-items: center;
  gap: var(--sp-2);
  padding: var(--sp-2) var(--sp-3);
  border: 1px solid var(--border);
  border-radius: var(--rd-4);
  background: var(--bg-surface);
}
.smc-title { flex: 1; font-size: var(--fs-sm); color: var(--text-strong); }
.sm-tasks { display: flex; flex-direction: column; gap: var(--sp-3); max-height: 60vh; overflow-y: auto; }
.sm-task {
  border: 1px solid var(--border);
  border-left: 3px solid var(--border-strong);
  border-radius: var(--rd-4, 6px);
  padding: var(--sp-3) var(--sp-4);
  display: flex;
  flex-direction: column;
  gap: var(--sp-2);
}
.sm-task.st-in_progress { border-left-color: var(--info); }
.sm-task.st-review { border-left-color: var(--warn); }
.sm-task.st-done { border-left-color: var(--pos-strong); }
.sm-task.st-returned { border-left-color: var(--neg-strong); }
.smt-head { display: flex; align-items: center; gap: var(--sp-2); flex-wrap: wrap; }
.smt-title { font-size: var(--fs-sm); }
.smt-meta { display: flex; flex-wrap: wrap; gap: var(--sp-3); font-size: var(--fs-2xs); color: var(--text-secondary); }
.smt-meta > span { display: inline-flex; align-items: center; gap: 3px; }
.smt-filled { color: var(--pos-strong); }
.smt-form { font-family: var(--font-mono); color: var(--text-muted); }
.smt-act { display: flex; gap: var(--sp-2); }
.link { color: var(--accent); }
.sc-meta {
  font-size: var(--fs-2xs);
  color: var(--text-muted);
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
  min-width: 0;
  overflow-wrap: anywhere;
}
.sc-dep {
  color: var(--warn);
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.sc-due {
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.sc-actions {
  display: flex;
  gap: var(--sp-3);
  /* Пара «Согласовать + Вернуть» в узкой колонке не помещалась в строку и
     выходила за границу карточки — переносим на вторую строку. */
  flex-wrap: wrap;
}
/* Базовая .btn — nowrap и фиксированная высота: длинная подпись не переносится
   и вылезает наружу. В колонке таймлайна ширина задаётся сеткой, поэтому здесь
   кнопке разрешено переносить текст и расти в высоту. */
.sc-actions .btn {
  min-width: 0;
  max-width: 100%;
  height: auto;
  min-height: 28px;
  padding-top: 3px;
  padding-bottom: 3px;
  white-space: normal;
  text-align: left;
  overflow-wrap: anywhere;
}
.sc-forms {
  display: flex;
  flex-wrap: wrap;
  gap: var(--sp-3);
  padding-top: var(--sp-2);
  border-top: 1px dashed var(--border);
}
.sc-formlink {
  font-size: var(--fs-2xs);
  color: var(--accent);
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
/* ── формы периода (карточки процесса) ── */
.cards-block {
  margin-top: var(--sp-6);
  border-top: 1px solid var(--border);
  padding-top: var(--sp-5);
  display: flex;
  flex-direction: column;
  gap: var(--sp-3);
}
.cb-head { display: flex; align-items: baseline; gap: var(--sp-4); flex-wrap: wrap; }
.cb-title { font-size: var(--fs-md); color: var(--text-strong); margin: 0; }
.cb-hint { font-size: var(--fs-2xs); color: var(--text-muted); }
.cb-empty { font-size: var(--fs-sm); color: var(--text-muted); margin: 0; }
.banner-warn { background: var(--warn-soft); color: var(--warn); }
.fcard {
  border: 1px solid var(--border);
  border-radius: var(--rd-4, 6px);
  padding: var(--sp-3) var(--sp-4);
  display: flex;
  flex-direction: column;
  gap: var(--sp-3);
}
.fc-main { display: flex; align-items: center; gap: var(--sp-3); flex-wrap: wrap; }
.fc-id { display: flex; flex-direction: column; gap: 1px; margin-right: auto; }
.fc-title { font-size: var(--fs-sm); font-weight: var(--fw-semibold); color: var(--text-strong); }
.fc-meta { display: flex; gap: var(--sp-2); flex-wrap: wrap; font-size: var(--fs-2xs); color: var(--text-muted); }
.fc-form { font-family: var(--font-mono); }
.fc-lock { display: inline-flex; align-items: center; gap: 3px; font-size: var(--fs-2xs); color: var(--neg); }
.fc-links { display: flex; gap: var(--sp-4); flex-wrap: wrap; }
.fc-link {
  font-size: var(--fs-2xs);
  color: var(--accent);
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.btn-like { border: 0; background: none; padding: 0; cursor: pointer; font: inherit; font-size: var(--fs-2xs); color: var(--accent); }

.tl-legend {
  display: flex;
  gap: var(--sp-6);
  margin-top: var(--sp-5);
  font-size: var(--fs-xs);
  color: var(--text-secondary);
}
.tl-legend .dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 999px;
  margin-right: 4px;
  background: var(--border-strong);
}
.dot.s-in_progress {
  background: var(--accent);
}
.dot.s-completed {
  background: var(--pos);
}
.dot.s-returned {
  background: var(--neg);
}

/* ── свод ── */
.empty-cell {
  text-align: center;
  color: var(--text-muted);
  padding: var(--sp-7) 0;
}
.le-cell {
  font-weight: var(--fw-medium);
}
.strong {
  font-weight: var(--fw-semibold);
}
.prov-note {
  font-size: var(--fs-xs);
  color: var(--text-muted);
  margin-top: var(--sp-4);
}
.pnl-head { display: flex; align-items: center; justify-content: space-between; gap: var(--sp-4); flex-wrap: wrap; }
.pnl-actions { display: flex; align-items: center; gap: var(--sp-3); }
.hidden-file { display: none; }
.banner-pos { background: var(--pos-soft); color: var(--pos-strong); }

/* ── модалка поля ── */
.field {
  display: block;
  margin-bottom: var(--sp-5);
}
.field-label {
  display: block;
  font-size: var(--fs-sm);
  color: var(--text-secondary);
  margin-bottom: var(--sp-3);
}
.req {
  color: var(--neg);
}
.field-hint {
  display: block;
  margin-top: var(--sp-2);
  font-size: var(--fs-xs);
  color: var(--text-muted);
}
.textarea {
  width: 100%;
  resize: vertical;
  font-family: var(--font-sans);
}
</style>
