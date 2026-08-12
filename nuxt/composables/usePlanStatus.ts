/**
 * Единый словарь состояний модуля планов.
 *
 * До этого было два несогласованных набора: этапы (pending/in_progress/completed/
 * returned) и задания (pending/in_progress/review/done/returned) — одно и то же
 * состояние называлось по-разному и рисовалось одинаково. Здесь одна шкала, один
 * цвет и одна иконка на состояние; экраны берут подписи только отсюда.
 */

export type PlanState = "pending" | "in_progress" | "review" | "done" | "returned" | "blocked" | "overdue" | "archived";

export interface StateMeta {
  label: string;
  badge: string; // класс бейджа из дизайн-системы
  icon: string;
  hint: string;
}

const STATES: Record<PlanState, StateMeta> = {
  pending: { label: "не начато", badge: "badge-dot", icon: "lucide:circle", hint: "никто не приступал" },
  in_progress: { label: "в работе", badge: "badge-info", icon: "lucide:circle-dot", hint: "исполнитель начал заполнение" },
  review: { label: "на проверке", badge: "badge-warn", icon: "lucide:eye", hint: "сдано, ждёт приёмки" },
  done: { label: "готово", badge: "badge-pos", icon: "lucide:check", hint: "принято/согласовано" },
  returned: { label: "возвращено", badge: "badge-neg", icon: "lucide:corner-up-left", hint: "вернули на доработку" },
  blocked: { label: "заблокировано", badge: "badge-dot", icon: "lucide:lock", hint: "ждёт другой этап (WF-DEP)" },
  overdue: { label: "просрочено", badge: "badge-neg", icon: "lucide:alarm-clock", hint: "срок этапа прошёл" },
  archived: { label: "архив", badge: "badge-dot", icon: "lucide:archive", hint: "период закрыт" }
};

/** Нормализация сырых статусов (этапы и задания) к единой шкале. */
export const toPlanState = (raw: string): PlanState => {
  switch (raw) {
    case "completed":
    case "approved":
    case "done":
      return "done";
    case "in_progress":
      return "in_progress";
    case "review":
      return "review";
    case "returned":
      return "returned";
    case "waiting_dependency":
    case "blocked":
      return "blocked";
    case "archived":
      return "archived";
    default:
      return "pending";
  }
};

export const usePlanStatus = () => {
  const meta = (raw: string): StateMeta => STATES[toPlanState(raw)];
  const label = (raw: string) => meta(raw).label;
  const badge = (raw: string) => meta(raw).badge;
  const icon = (raw: string) => meta(raw).icon;

  /** Дней до дедлайна: <0 — просрочено, 0 — сегодня. null, если срока нет. */
  const daysLeft = (due?: string | null): number | null => {
    if (!due) return null;
    const d = new Date(due + "T00:00:00");
    if (Number.isNaN(d.getTime())) return null;
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    return Math.round((d.getTime() - today.getTime()) / 86400000);
  };

  /** Человеческая подпись срока: «просрочено на 2 дня» / «сегодня» / «через 3 дня». */
  const dueLabel = (due?: string | null): string => {
    const n = daysLeft(due);
    if (n === null) return "";
    if (n < 0) return `просрочено на ${plural(-n)}`;
    if (n === 0) return "срок сегодня";
    return `через ${plural(n)}`;
  };

  /** Просрочено ли: срок прошёл, а состояние не финальное. */
  const isOverdue = (due: string | null | undefined, raw: string): boolean => {
    const n = daysLeft(due);
    return n !== null && n < 0 && toPlanState(raw) !== "done";
  };

  return { meta, label, badge, icon, daysLeft, dueLabel, isOverdue, states: STATES };
};

const plural = (n: number): string => {
  const last = n % 10;
  const teen = n % 100 >= 11 && n % 100 <= 14;
  if (!teen && last === 1) return `${n} день`;
  if (!teen && last >= 2 && last <= 4) return `${n} дня`;
  return `${n} дней`;
};
