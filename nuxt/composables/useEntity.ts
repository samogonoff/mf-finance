/**
 * Контекст выбранного юр.лица (multi-tenant). Финансовый кабинет работает
 * сразу с группой компаний из нескольких стран — выбранное юр.лицо
 * фильтрует все цифры (KPI, операции, отчёты, контрагенты).
 *
 * Сохраняем выбор в cookie, чтобы:
 *   - SSR отдавал HTML с тем же контекстом (без «прыжка» при гидрации)
 *   - перезагрузка страницы не сбрасывала фильтр
 *
 * "all-{countryCode}" — псевдо-сущность «все юр.лица страны»
 * "all"               — псевдо-сущность «вся группа» (консолидированно)
 *
 * В реальности список будет приходить с Go-API (`GET /api/entities`),
 * с правами по юзеру. Пока — mock в коде, реальные роли подключим
 * вместе с API.
 */

export interface Country {
  code: "RU" | "BY" | "KZ";
  name: string;
  flag: string;
  currency: "RUB" | "BYN" | "KZT";
}

export interface Entity {
  id: string;            // 'ru-1', 'by-2', ...
  countryCode: Country["code"];
  legalForm: string;     // ООО, ИП, ТОО
  name: string;          // «Группа-РФ»
  inn: string;
  active: boolean;
}

export const COUNTRIES: Country[] = [
  { code: "RU", name: "Россия",     flag: "🇷🇺", currency: "RUB" },
  { code: "BY", name: "Беларусь",   flag: "🇧🇾", currency: "BYN" },
  { code: "KZ", name: "Казахстан",  flag: "🇰🇿", currency: "KZT" }
];

export const ENTITIES: Entity[] = [
  // RU
  { id: "ru-1", countryCode: "RU", legalForm: "ООО", name: "Группа-РФ",         inn: "7723456789",   active: true },
  { id: "ru-2", countryCode: "RU", legalForm: "ООО", name: "Сервис-Логистик",   inn: "5024118327",   active: true },
  { id: "ru-3", countryCode: "RU", legalForm: "ООО", name: "Диджитал-РФ",       inn: "7728164290",   active: true },
  { id: "ru-4", countryCode: "RU", legalForm: "ИП",  name: "Петров А.В.",       inn: "504801234567", active: true },

  // BY
  { id: "by-1", countryCode: "BY", legalForm: "ООО", name: "Группа-БЛР",        inn: "100428376",    active: true },
  { id: "by-2", countryCode: "BY", legalForm: "УП",  name: "Логистика-Минск",   inn: "190928422",    active: true },

  // KZ
  { id: "kz-1", countryCode: "KZ", legalForm: "ТОО", name: "Группа-KZ",         inn: "061040002847", active: true },
  { id: "kz-2", countryCode: "KZ", legalForm: "ТОО", name: "Алматы-Сервис",     inn: "180340004928", active: false }
];

export type Selection =
  | { kind: "all" }                                  // вся группа
  | { kind: "country"; code: Country["code"] }       // все юр.лица страны
  | { kind: "entity"; id: string };                  // одно юр.лицо

const DEFAULT_SELECTION: Selection = { kind: "entity", id: "ru-1" };

function parse(raw: string | null | undefined): Selection {
  if (!raw) return DEFAULT_SELECTION;
  if (raw === "all") return { kind: "all" };
  if (raw.startsWith("country:")) {
    const code = raw.slice(8) as Country["code"];
    return { kind: "country", code };
  }
  if (raw.startsWith("entity:")) {
    return { kind: "entity", id: raw.slice(7) };
  }
  return DEFAULT_SELECTION;
}

function stringify(s: Selection): string {
  if (s.kind === "all") return "all";
  if (s.kind === "country") return `country:${s.code}`;
  return `entity:${s.id}`;
}

export const useEntity = () => {
  const cookie = useCookie<string>("fin.entity", {
    default: () => stringify(DEFAULT_SELECTION),
    sameSite: "lax",
    path: "/",
    maxAge: 60 * 60 * 24 * 365
  });

  const selection = useState<Selection>("fin-entity", () => parse(cookie.value));

  const setSelection = (s: Selection) => {
    selection.value = s;
    cookie.value = stringify(s);
  };

  // Для удобства потребителей: текущий список «активных» entity
  const visibleEntities = computed<Entity[]>(() => {
    const s = selection.value;
    if (s.kind === "all") return ENTITIES;
    if (s.kind === "country") return ENTITIES.filter((e) => e.countryCode === s.code);
    return ENTITIES.filter((e) => e.id === s.id);
  });

  // Подпись для UI: "ООО «Группа-РФ»" / "Все юр.лица РФ" / "Вся группа"
  const label = computed<string>(() => {
    const s = selection.value;
    if (s.kind === "all") return "Вся группа компаний";
    if (s.kind === "country") {
      const c = COUNTRIES.find((c) => c.code === s.code);
      return `Все юр.лица · ${c?.name ?? s.code}`;
    }
    const e = ENTITIES.find((x) => x.id === s.id);
    return e ? `${e.legalForm} «${e.name}»` : "—";
  });

  // Подпись страны для UI (флаг + страна)
  const countryLabel = computed<{ flag: string; name: string; code: string } | null>(() => {
    const s = selection.value;
    if (s.kind === "all") return null;
    const code = s.kind === "country" ? s.code : ENTITIES.find((e) => e.id === s.id)?.countryCode;
    const c = COUNTRIES.find((cc) => cc.code === code);
    return c ? { flag: c.flag, name: c.name, code: c.code } : null;
  });

  // Условный множитель для mock-данных. На реальных данных НЕ нужен —
  // фильтр будет на бэке, а тут он просто меняет цифры, чтобы было видно
  // что переключение «работает».
  const mockMultiplier = computed<number>(() => {
    const s = selection.value;
    if (s.kind === "all") return 1.0;
    if (s.kind === "country") {
      return s.code === "RU" ? 0.72 : s.code === "BY" ? 0.18 : 0.10;
    }
    // одна компания
    const idx = ENTITIES.findIndex((e) => e.id === s.id);
    return 0.08 + (idx % 5) * 0.07; // 0.08…0.36
  });

  return { selection, setSelection, visibleEntities, label, countryLabel, mockMultiplier };
};
