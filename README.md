# ClawMind

<p align="center">
  <img src="docs/assets/logo.svg" width="88" height="88" alt="ClawMind logo" />
</p>

<p align="center">
  <strong>A local-first coding agent for real repositories</strong><br/>
  Go (Gin + SQLite + SSE) · Vue 3 (Vite + Pinia + Tailwind) · <a href="README_CN.md">中文文档</a>
</p>

---

ClawMind is not a general-purpose "do any task" assistant. It is scoped to **code work in a codebase**: it profiles a repository, locates the relevant symbols and call sites, makes exact edits, and then proves the change by running the project's own verification command. On top of that it carries a **self-evolving harness** — the agent may change how it works, but never the repository code.

Everything runs locally: a single Go binary, a pure-Go SQLite database (no CGO), and a Vue 3 frontend.

## Highlights

- **Code-first loop** — locate → edit → verify → wrap up. A claimed fix is checked against the project, not asserted.
- **Bounded repository awareness** — language mix, manifests, inferred `test`/`build`/`lint`/`format` commands and a capped file tree, without shelling out.
- **Editable harness surface** — `AGENTS.md`, `tool_guidance.md`, `lessons.md`, `skills/`, `scripts/`. Always-on text is the most expensive component, so knowledge is pushed into on-demand skills and scripts.
- **Evidence-constrained evolution** — Propose → Probe → Compose → blind Select → Reflect, with per-edit verdicts (`helped` / `hurt` / `no observed effect` / `confounded`). Keeping the baseline is a legitimate outcome.
- **Transferable experience library** — `cue` → `lesson` per project, generalized rather than memorized, and treated as data, not as instructions.
- **Software-engineering skills** — five priority skills (debugging, TDD, verification, review, architecture) plus one workflow chain: Requirement → Architecture → Plan → TDD → Review → Verification → Release. A completion claim needs verifiable evidence, not a self-report.
- **Plugin layers** — every part of the product is a plugin; registrations are reversible effects and layer order decides which one wins. A plugin can only tighten the safety policy, never loosen it.
- **Layered memory (L0–L3)** — conversation → atom → scenario → persona, distilled layer by layer; retrieval fuses BM25 + vector + RRF under item, character and time budgets. Ownership, versioning, visibility, ACL and bindings exist once in an asset registry.
- **Streaming UI** — ChatGPT-style main column, collapsible task-flow and thinking segments, tool steps, GFM tables, code copy, KaTeX.
- **Safety boundaries** — workspace root, blocked reads of local config/key files, human approval for high-risk shell, SSRF filtering on `web_fetch`. A patch layer can add deny patterns and lower limits, but cannot remove the built-in floor.

## Quick start

Prerequisites: Go 1.22+, Node.js 18+ (20+ recommended), and an OpenAI-compatible Chat Completions endpoint.

```bash
cp .env.example .env          # optional; the server also reads OS env
make run                      # backend :8080 + frontend :5173
```

Open <http://127.0.0.1:5173> (Vite proxies `/api` to `:8080`). Run the two halves separately with `make dev-backend` / `make dev-frontend`.

Enable the self-evolution engine:

```bash
CLAWMIND_EVOLUTION=1 make run
```

Then open the **Harness** panel from the bottom of the sidebar. The **Plugins & memory** panel shows each plugin's layer order, the resolved config, and the layer census; the settings page lists the keys a plugin layer overrides, because saving writes the file rather than the value in effect.

## Code-first workflow

The system prompt encodes four rules (`backend/internal/agent/prompts.go`, `backend/data/prompts/`):

1. **Locate before editing** — search and read the context; never assume file contents.
2. **Edit exactly** — `file_edit` fails when the target text is absent or ambiguous, which forces a re-read instead of a guess.
3. **Verify after editing** — the inferred test command runs after any round that touched files, and failures are fed back into the run (`backend/internal/agent/verify.go`).
4. **Deliverables belong to the user** — the shape of the output is the user's decision; the experience library is data, not instructions.

## Tools

Built-in atomic tools: `file_read`, `file_write`, `shell_exec`, `web_fetch`, `task_plan`, `task_summary`.

Code tools (`backend/internal/tools/code.go`, executed by `backend/internal/agent/code_tools.go`):

| Tool | Purpose | Bound |
|------|---------|-------|
| `code_search` | Literal or regex search, `file:line` matches | Max results (default 60); skips dependency and build dirs |
| `repo_tree` | Repository structure | Depth 3, 240 entries |
| `file_edit` | Exact string replacement | Fails on missing or ambiguous `old` |
| `run_tests` | Run the inferred verification command | Restricted to `AGENT_WORKSPACE`; high-risk shell needs approval |
| `load_skill` | Read an on-demand harness skill | Read-only, zero cost until triggered |
| `delegate` | Run one harness subagent in its own context | Answer capped at 4000 characters |

Custom skills can be imported as OpenAI-tools JSON or created in the sidebar; they merge with `TOOLS_PATH`, plugin contributions and the built-ins. Community `SKILL.md` layouts import through `POST /api/skills/import-md`: `name`, `description` and `mode` are parsed from the frontmatter, the whole tree is validated before the write, and a skill missing a trigger line is fixed by `AutoTrigger()` — a skill never named in always-on text is never opened.

The five priority skills and `scripts/preflight.py` come from the seeded surface (`CLAWMIND_SEED_SKILLS=1` or `POST /api/harness/seed`). Details: [docs/skills-roadmap.md](docs/skills-roadmap.md).

## Plugin layers

Plugins compose in directory order (`CLAWMIND_PLUGIN_DIR`, default `.clawmind/plugins`). Known seams: `llm`, `tools`, `memory`, `session`, `sandbox`, `prompt`, `hooks`. `plugin.Resolve()` turns the patch layers into the values the host actually reads at boot; a patch key the host never reads is rejected at validation instead of silently ignored. Hook exit code `2` is the documented veto. Details: [docs/plugins.md](docs/plugins.md).

## Memory tiers

L0 conversation (raw turns) → L1 atom (facts) → L2 scenario (project knowledge blocks) → L3 persona (long-term profile). Writes keep only transferable process, not the specifics of this task. Retrieval has two paths: bootstrap layers (L2, L3) give quick context, fallback layers (L0, L1) are recalled with BM25 + vector + RRF when specific facts are needed, under budgets of 24 items / 2000 characters / 3 seconds. Reference: [TencentDB-Agent-Memory](https://github.com/TencentCloud/TencentDB-Agent-Memory).

## Self-evolution

Two levels of recursion (`backend/internal/harness`, `backend/internal/evolution`):

- **Task level** — one Propose–Probe–Compose–Select–Reflect step over the current harness, in `parallel`, `sequential` or `dry` mode. Selection is blind: candidates are shown under anonymized ids.
- **Harnessing level** — a per-project experience library (`INDEX.md` + `harnesses/<slug>/EXPERIENCE.md`) with `cue`, `lesson`, `evidence`, `confidence`.

Before anything is written, the whole tree is validated; a proposal that fails validation is discarded in full. Probe is a model assessment unless `CLAWMIND_PROBE_EXEC=1`, in which case the candidate tree is written to an isolated directory, its scripts run, and the inferred test command is executed — nothing is credited without something having run. Selection is blind: ids come from a seeded shuffle, the pool is exactly the k finals, and every drop is recorded with its cause. The budget identity (R = 4k, P = 3k) is recorded as an error when a round does not land on it, and a completed run can only be reused when a runmark names the same configuration. Details: [docs/self-evolution.md](docs/self-evolution.md).

## Configuration

| Variable | Default | Notes |
|----------|---------|-------|
| `OPENAI_API_KEY` / `OPENAI_BASE_URL` / `OPENAI_MODEL` | — | Fallback when `config.json` is empty |
| `LISTEN` | `:8080` | HTTP listen address |
| `DB_PATH` | `./data/clawmind.db` | SQLite path |
| `CLAWMIND_DIR` | `.clawmind/` | `config.json`, `skills.json` |
| `TOOLS_PATH` | `./config/tools.json` | Extra tool definitions |
| `AGENT_WORKSPACE` | — | Root for file and shell tools |
| `CLAWMIND_CODE_PROFILE` | `true` | Inject the repository profile |
| `CLAWMIND_AUTO_VERIFY` | `true` | Run the inferred test command after edits |
| `CLAWMIND_EVOLUTION` | `false` | Enable the evolution engine |
| `CLAWMIND_EVOLUTION_K` | `3` | Candidates / sequential rounds (1–8) |
| `CLAWMIND_EVOLUTION_MODE` | `parallel` | `parallel` · `sequential` · `dry` |
| `CLAWMIND_HARNESS_DIR` | `.clawmind/harness` | Harness root, one subdirectory per project |
| `CLAWMIND_MEMORY_BACKEND` | `sqlite` | `memory` = in-process, useful for tests |
| `CLAWMIND_EMBEDDING_MODEL` | — | Enables the vector branch when set (otherwise BM25 + RRF) |
| `CLAWMIND_MEMORY_MAX_ITEMS` | `24` | Retrieval item budget |
| `CLAWMIND_MEMORY_CHAR_BUDGET` | `2000` | Retrieval character budget |
| `CLAWMIND_MEMORY_TIMEOUT_MS` | `3000` | Retrieval timeout |
| `CLAWMIND_MEMORY_L0_CAP`..`L3_CAP` | `400/150/100/200` | Per-layer row caps |
| `CLAWMIND_MEMORY_DISTILL_ASYNC` | `true` | Distillation does not block the response |
| `CLAWMIND_PLUGIN_DIR` | `.clawmind/plugins` | Plugin home |
| `CLAWMIND_PROBE_EXEC` | `false` | Probes really execute (scripts + inferred test command) |
| `CLAWMIND_EVOLUTION_LEDGER` | `true` | Record a budget-identity miss as an error |
| `CLAWMIND_EVOLUTION_RESUME` | `true` | Reuse a completed run with the same configuration |
| `CLAWMIND_SEED_SKILLS` | `true` | Seed the software-engineering surface into an empty harness |
| `CLAWMIND_MAX_CONTEXT_TOKENS` | `24000` | History token ceiling |
| `CLAWMIND_TOKEN_BUDGET` | `0` | Soft per-response ceiling |
| `CLAWMIND_MCP_COMMAND` / `_ARGS` / `_ENV` | — | MCP stdio server (empty disables it) |

## API

- `GET /api/health`
- `GET/PUT /api/settings` — `config.json`; `GET` also returns `effective` (what the runtime actually uses) and `source` (which plugin wins each key)
- `GET/POST /api/skills`, `POST /api/skills/import`
- `GET/POST /api/projects`, `PATCH/DELETE /api/projects/:id`
- `GET/POST /api/sessions`, `PATCH/DELETE /api/sessions/:id`
- `GET/POST /api/sessions/:id/messages`, `POST .../messages/:mid/regenerate`, `POST .../messages/:mid/cancel`
- `GET /api/sessions/:id/stream` — SSE (`part_start` / `delta` / `part_end` / `tool_call` / `tool_result` / `tool_approval_request` / `done` / `error`)
- `POST /api/sessions/:id/tool-approval`
- Harness: `GET /api/harness`, `POST /api/harness/seed`, `GET/POST /api/harness/file`, `POST /api/harness/round`, `GET /api/harness/rounds`, `POST /api/harness/adopt`, `POST /api/harness/lessons`, `GET /api/harness/library`
- Plugins: `GET/POST /api/plugins`, `POST /api/plugins/:id/enable`, `POST /api/plugins/:id/disable`, `GET /api/plugins/config`
- Memory: `GET /api/memory/layers`, `GET /api/memory/search`, `POST /api/memory/distill`, `GET/POST /api/memory/assets`, `GET /api/memory/assets/:id`, `POST /api/memory/assets/:id/{share,status,acl,acl/revoke,bind}`, `POST /api/memory/unbind/:bindingId`, `GET /api/memory/loadout`
- Skills: `POST /api/skills/import-md` (community `SKILL.md` layout)

## Makefile

```bash
make deps            # npm ci
make backend-build   # bin/clawmind-server (pure-Go SQLite, no CGO)
make frontend-build  # frontend/dist
make build           # all three
make test            # go test ./...
make vet             # go vet ./...
make fmt             # gofmt -w
make docker-up       # docker compose up --build -d
```

## Docker

```bash
docker compose up --build
```

Frontend on <http://localhost:8081>, backend on `:8080`, data in the `clawmind-data` volume.

## Docs

| Document | Contents |
|----------|----------|
| [docs/code-agent.md](docs/code-agent.md) | Positioning, code tools, repository profile, verification gate |
| [docs/self-evolution.md](docs/self-evolution.md) | Recursion levels, editable surface, validation rules, endpoints, env vars |
| [docs/features.md](docs/features.md) | Product features, memory, UI |
| [docs/architecture.md](docs/architecture.md) | Modules, data flow, configuration |
| [docs/architecture-evolution.md](docs/architecture-evolution.md) | Target architecture, SSE/MCP contracts |
| [docs/skills-roadmap.md](docs/skills-roadmap.md) | Software-engineering skills, trigger-line rule, `SKILL.md` compatibility |
| [docs/plugins.md](docs/plugins.md) | Plugin layers, seam contract, hook events and the veto |

## Notes and limits

- `shell_exec` runs on the host. `AGENT_WORKSPACE` narrows the blast radius, and high-risk commands require confirmation, but this is **not** a container-level sandbox.
- Keep API keys in environment variables. `file_read` refuses to read the local config directory, and the agent is instructed not to echo secrets.
- Evolution writes only to the editable harness surface; it cannot modify repository code.
- Plugin layers can only tighten the safety policy: they add deny or high-risk patterns, lower timeouts and always-on budgets, and narrow the workspace root. The built-in floor cannot be removed and the workspace root cannot be widened. Hook commands run inside the plugin home, and a high-risk hook is refused unless approval is explicitly allowed.

## License

See [LICENSE](LICENSE).
