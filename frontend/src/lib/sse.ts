import type { StreamEvent } from "@/types/chat";

/** Parses SSE lines from a chunk of bytes. Returns leftover incomplete line prefix. */
export function parseSSEChunk(
  buffer: string,
  chunk: string,
  onEvent: (ev: StreamEvent) => void
): string {
  const combined = buffer + chunk;
  const lines = combined.split("\n");
  const incomplete = lines.pop() ?? "";
  for (const line of lines) {
    const trimmed = line.trimEnd();
    if (!trimmed.startsWith("data:")) continue;
    const payload = trimmed.slice(5).trim();
    if (!payload) continue;
    try {
      const ev = JSON.parse(payload) as StreamEvent;
      onEvent(ev);
    } catch {
      /* ignore */
    }
  }
  return incomplete;
}
