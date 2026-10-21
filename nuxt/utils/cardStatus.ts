// Единые метки статуса карточки формы: «Новый → Выполняется → Закрыто». Технические статусы сервера
// (draft, on_approval, returned, approved…) пользователю ничего не говорят.
export interface CardStatusInput {
  status: string;
  step_code?: string;
  status_label?: string;
  /** У черновика формы расходов есть сохранённые суммы (приходит с карточкой). */
  has_data?: boolean;
}

export function cardStatusText(c: CardStatusInput): string {
  switch (c.status) {
    case "draft": return c.has_data ? "Выполняется" : "Новый";
    case "on_approval": return `Выполняется · на согласовании (шаг ${c.step_code})`;
    case "returned": return `Выполняется · возвращено на доработку (шаг ${c.step_code})`;
    case "approved": return "Закрыто";
    case "published": return "Закрыто · опубликовано";
    // Этап 1.6 такую карточку не считает закрытой (сервер: leCardClosed), поэтому и подпись не «Закрыто».
    case "publish_failed": return "Выполняется · ошибка публикации";
    case "archived": return "Архив";
    default: return c.status_label || c.status;
  }
}

export function cardStatusTone(status: string): string {
  switch (status) {
    case "on_approval": return "badge-warn";
    case "returned": case "publish_failed": return "badge-neg";
    case "approved": case "published": return "badge-pos";
    case "archived": return "badge-dot";
    default: return "badge-info";
  }
}
