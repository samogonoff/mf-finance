import os from "node:os";
import {shipLog} from "./logstash";

/**
 * serverLog — структурный лог серверных роутов Nitro. Пишет одну JSON-строку в
 * stdout (базовый канал, его собирает docker) и дублирует в Logstash/ELK через
 * shipLog. Форма записи совпадает с логами Go-API и python-cost
 * (time/level/msg/service/host + свои поля), чтобы в общем индексе ELK рантаймы
 * кабинета лежали единообразно. service=finance-nuxt отличает фронтовый рантайм
 * от finance-api и finance-cost.
 */

const HOSTNAME = os.hostname();

type Level = "INFO" | "WARN" | "ERROR";

export function serverLog(
  level: Level,
  msg: string,
  fields: Record<string, unknown> = {}
): void {
  const record = {
    time: new Date().toISOString(),
    level,
    msg,
    service: "finance-nuxt",
    host: HOSTNAME,
    ...fields
  };
  const line = JSON.stringify(record);
  // stdout — базовый канал; ERROR в stderr, остальное в stdout.
  if (level === "ERROR") console.error(line);
  else console.log(line);
  shipLog(line + "\n"); // json_lines: одна запись на строку
}
