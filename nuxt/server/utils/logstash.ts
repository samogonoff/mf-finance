import net from "node:net";

/**
 * Асинхронная отправка строк лога в Logstash по TCP (кодек json_lines) — те же
 * инварианты, что у Go-шиппера (go/internal/logship): shipLog НИКОГДА не
 * блокирует и не бросает, при недоступности Logstash строки отбрасываются, а
 * фон переподключается с backoff. Базовый канал — stdout (его собирает docker) —
 * остаётся всегда; сюда льётся дубль.
 *
 * LOGSTASH_HOST / LOGSTASH_PORT читаются из process.env НАПРЯМУЮ, а не через
 * runtimeConfig. Причина принципиальная: runtimeConfig запекается на build, и
 * его приватные значения переопределяются в рантайме только именем NUXT_<КЛЮЧ>
 * (та же грабля, что с B24_CLIENT_SECRET). Server-only код в Node видит
 * process.env вживую — swarm/nuxt/entrypoint.sh экспортит .env в окружение
 * процесса, — поэтому имя без префикса работает и остаётся ОБЩИМ с Go-API.
 */

const HOST = process.env.LOGSTASH_HOST || "";
const PORT = Number(process.env.LOGSTASH_PORT || "5044");
const MAX_QUEUE = 4096;

let socket: net.Socket | null = null;
let connecting = false;
let reconnectTimer: NodeJS.Timeout | null = null;
let backoff = 1000;
const queue: string[] = [];
let dropped = 0;

function scheduleReconnect() {
  if (reconnectTimer || !HOST) return;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connect();
  }, backoff);
  reconnectTimer.unref?.(); // не держать event loop ради реконнекта при shutdown
  backoff = Math.min(backoff * 2, 30000);
}

function connect() {
  if (socket || connecting || !HOST) return;
  connecting = true;
  const s = new net.Socket();
  s.setNoDelay(true);
  s.on("connect", () => {
    connecting = false;
    backoff = 1000;
    socket = s;
    flush();
  });
  // error всегда сопровождается close — реконнект вешаем только на close,
  // чтобы не планировать его дважды.
  s.on("error", () => {});
  s.on("close", () => {
    connecting = false;
    if (socket === s) socket = null;
    scheduleReconnect();
  });
  s.unref(); // HTTP-листенер держит процесс живым; сокет лога — нет
  s.connect(PORT, HOST);
}

function flush() {
  if (!socket) return;
  while (queue.length) {
    const line = queue.shift()!;
    // write возвращает false при переполнении буфера ядра, но строку УЖЕ
    // приняла (буферизует Node) — не теряем, ждём drain и продолжаем.
    if (!socket.write(line)) {
      socket.once("drain", flush);
      return;
    }
  }
}

/** shipLog кладёт готовую строку (с '\n' на конце) в очередь. Не блокирует. */
export function shipLog(line: string): void {
  if (!HOST) return;
  if (queue.length >= MAX_QUEUE) {
    dropped++;
    return;
  }
  queue.push(line);
  if (socket) flush();
  else connect();
}

/** droppedCount — сколько строк потеряно (очередь переполнена). */
export function droppedCount(): number {
  return dropped;
}
