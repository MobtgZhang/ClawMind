package evolution

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/harness"
)

// writeIndex regenerates INDEX.md for a scope library from what is on disk.
//
// Retrieval is by construction: a scope directory only ever holds same-scope entries, so a
// lesson recorded here is read by the next task in the same project.
func writeIndex(dir string, res *Result) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rows := []string{
		"# 经验库",
		"",
		"每一条都是同一项目中此前某个任务被采纳的 harness，连同完整组件树与选择理由存储。",
		"先浏览本索引，再打开与当前任务相关的那几条。",
		"",
	}
	if res != nil && len(res.Final) > 0 {
		rows = append(rows, "## 当前生效的 harness", "", "- 目录：`"+dir+"` — "+res.Final.Inventory(), "")
	}
	entries, err := os.ReadDir(filepath.Join(dir, "harnesses"))
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		entries = nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		rows = append(rows, "_(空 — 这是该项目的第一个被采纳的 harness)_", "")
	}
	for _, name := range names {
		ed := filepath.Join(dir, "harnesses", name)
		t, err := harness.Read(filepath.Join(ed, "harness"))
		if err != nil {
			continue
		}
		rows = append(rows, fmt.Sprintf("## `%s/`", name))
		rows = append(rows, fmt.Sprintf("- harness: `%s/harness/` — %s", name, t.Inventory()))
		rows = append(rows, fmt.Sprintf("- why:     `%s/EXPERIENCE.md`", name))
		if cue := field(filepath.Join(ed, "EXPERIENCE.md"), "cue"); cue != "" {
			rows = append(rows, "- cue:     "+cue)
		}
		rows = append(rows, "")
	}
	if res != nil && res.Lesson != nil {
		rows = append(rows, "## 本轮", "", fmt.Sprintf("- adopted: `%s`", res.Chosen),
			fmt.Sprintf("- components: %s", res.Final.Inventory()), "")
		if res.Round != nil && res.Round.Notes != "" {
			rows = append(rows, "### 为什么这样选", res.Round.Notes, "")
		}
		if len(res.Edits) > 0 {
			rows = append(rows, "### 每个改动做了什么", "")
			for _, e := range res.Edits {
				rows = append(rows, fmt.Sprintf("- **%s** (%s): %s → %s [%s]",
					e.Component, e.Candidates, e.Change, e.Observed, e.Verdict))
			}
			rows = append(rows, "")
		}
		rows = append(rows, "### 可迁移的教训", "")
		for _, f := range []string{"cue", "lesson", "evidence", "confidence"} {
			v := lessonValue(res.Lesson, f)
			if v == "" {
				continue
			}
			rows = append(rows, fmt.Sprintf("- **%s**: %s", f, v))
		}
		rows = append(rows, "")
	}
	return os.WriteFile(filepath.Join(dir, "INDEX.md"), []byte(strings.Join(rows, "\n")), 0o644)
}

// WriteExperience records why a harness was adopted for one task, next to its tree.
func (e *Engine) WriteExperience(projectID, taskID string, tree harness.Tree, why string, lesson *domain.HarnessLesson) error {
	dir := e.Dir(projectID)
	slug := strings.ReplaceAll(taskID, "/", "__")
	entry := filepath.Join(dir, "harnesses", slug)
	if err := os.MkdirAll(filepath.Join(entry, "harness"), 0o755); err != nil {
		return err
	}
	if err := tree.Write(filepath.Join(entry, "harness")); err != nil {
		return err
	}
	lines := []string{"# " + taskID, "", "- components: " + tree.Inventory(), ""}
	if strings.TrimSpace(why) != "" {
		lines = append(lines, "## 为什么选择", strings.TrimSpace(why), "")
	}
	if lesson != nil {
		lines = append(lines, "## 可迁移的教训")
		for _, f := range []string{"cue", "lesson", "evidence", "confidence"} {
			v := lessonValue(lesson, f)
			if v == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("- **%s**: %s", f, v))
		}
		lines = append(lines, "")
	}
	if err := os.WriteFile(filepath.Join(entry, "EXPERIENCE.md"), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return err
	}
	return writeIndex(dir, nil)
}

func lessonValue(l *domain.HarnessLesson, f string) string {
	if l == nil {
		return ""
	}
	switch f {
	case "cue":
		return l.Cue
	case "lesson":
		return l.Lesson
	case "evidence":
		return l.Evidence
	case "confidence":
		return l.Confidence
	}
	return ""
}

func field(path, name string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, fmt.Sprintf("- **%s**:", name)) {
			return strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		}
	}
	return ""
}

// Entry is one stored harness in the experience library.
type Entry struct {
	Slug      string    `json:"slug"`
	Inventory string    `json:"inventory"`
	Cue       string    `json:"cue,omitempty"`
	Lesson    string    `json:"lesson,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// ListEntries returns stored harness entries for a scope, newest first.
func (e *Engine) ListEntries(projectID string) ([]Entry, error) {
	dir := filepath.Join(e.Dir(projectID), "harnesses")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		entryDir := filepath.Join(dir, ent.Name())
		t, err := harness.Read(filepath.Join(entryDir, "harness"))
		if err != nil {
			continue
		}
		out = append(out, Entry{
			Slug:      ent.Name(),
			Inventory: t.Inventory(),
			Cue:       field(filepath.Join(entryDir, "EXPERIENCE.md"), "cue"),
			Lesson:    field(filepath.Join(entryDir, "EXPERIENCE.md"), "lesson"),
		})
		if info, err := ent.Info(); err == nil {
			out[len(out)-1].CreatedAt = info.ModTime().UTC()
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Adopt copies one stored entry into the live harness for the scope. A tree that fails
// validation is not adopted: a broken component is worse than no component.
func (e *Engine) Adopt(projectID, slug string) (harness.Tree, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" || strings.Contains(slug, "/") || strings.Contains(slug, "..") {
		return nil, fmt.Errorf("adopt: invalid entry %q", slug)
	}
	dir := e.Dir(projectID)
	t, err := harness.Read(filepath.Join(dir, "harnesses", slug, "harness"))
	if err != nil {
		return nil, err
	}
	if problems := t.Validate(); len(problems) > 0 {
		return nil, fmt.Errorf("adopt: %s", strings.Join(problems, "; "))
	}
	if err := t.Write(dir); err != nil {
		return nil, err
	}
	return t, writeIndex(dir, nil)
}
