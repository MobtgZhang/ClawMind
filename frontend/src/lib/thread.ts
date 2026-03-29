import type { Message } from "@/types/chat";

/** 与后端 thread.RootKey 一致 */
export const THREAD_ROOT = "__root__";

export function parseSiblingChoiceFromMeta(meta?: Record<string, string>): Record<string, string> {
  const raw = meta?.siblingChoice;
  if (!raw) return {};
  try {
    return JSON.parse(raw) as Record<string, string>;
  } catch {
    return {};
  }
}

export function parentKeyForMessage(parentMessageId: string | undefined | null): string {
  return parentMessageId && parentMessageId !== "" ? parentMessageId : THREAD_ROOT;
}

/** 同一用户只对应一条助手消息时取时间最早的一条（与按 createdAt 排序一致） */
function pickAssistantForUser(sorted: Message[], userId: string): Message | undefined {
  let best: Message | undefined;
  for (const m of sorted) {
    if (m.role !== "assistant" || m.parentMessageId !== userId) continue;
    if (!best || new Date(m.createdAt).getTime() < new Date(best.createdAt).getTime()) {
      best = m;
    }
  }
  return best;
}

/** 与后端 thread.LegacyLinearThread 一致：存在任一助手/用户的 parent 则走树路径 */
export function legacyLinearThread(messages: Message[]): boolean {
  if (messages.some((m) => m.role === "assistant" && m.parentMessageId)) {
    return false;
  }
  return !messages.some((m) => m.role === "user" && m.parentMessageId);
}

export function buildPathForSession(all: Message[], choice: Record<string, string>): Message[] {
  const sorted = [...all].sort(
    (a, b) => new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime()
  );
  if (legacyLinearThread(sorted)) {
    return sorted;
  }
  const children = new Map<string, Message[]>();
  for (const m of sorted) {
    if (m.role !== "user") continue;
    const k = parentKeyForMessage(m.parentMessageId);
    if (!children.has(k)) children.set(k, []);
    children.get(k)!.push(m);
  }
  const out: Message[] = [];
  let pk = THREAD_ROOT;
  for (;;) {
    const users = children.get(pk) ?? [];
    if (users.length === 0) break;
    const chosenId = choice[pk];
    let u = chosenId ? users.find((x) => x.id === chosenId) : undefined;
    if (!u) u = users[users.length - 1];
    out.push(u);
    const asst = pickAssistantForUser(sorted, u.id);
    if (!asst) break;
    out.push(asst);
    pk = asst.id;
  }
  return out;
}

/** 同一用户「轮次」下所有版本（同 branch、同父），按时间排序 */
export function userTurnVersions(all: Message[], m: Message): Message[] {
  const branchRoot = m.branchId ?? m.id;
  const p = m.parentMessageId ?? null;
  return all
    .filter(
      (x) =>
        x.role === "user" &&
        (x.branchId ?? x.id) === branchRoot &&
        (x.parentMessageId ?? null) === p
    )
    .sort((a, b) => new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime());
}

export function userVersionIndex(versions: Message[], m: Message): number {
  const i = versions.findIndex((v) => v.id === m.id);
  return i < 0 ? 0 : i;
}
