package agent

import (
	"fmt"
	"strings"

	"github.com/mobtgzhang/clawmind/backend/internal/memory"
)

// loadoutBlock renders the assets bound to this agent. Access control already decided what is
// visible here, so the block states only what the agent is expected to load by default; it is not
// a second permission check and must not read as one.
func loadoutBlock(assets []memory.Asset) string {
	if len(assets) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 资产装载（agent loadout）\n\n")
	for _, a := range assets {
		ver := strings.TrimSpace(a.Version)
		if ver == "" {
			ver = "1"
		}
		b.WriteString(fmt.Sprintf("- %s [%s · %s · v%s]\n", strings.TrimSpace(a.Name), a.Type, a.Status, ver))
	}
	return strings.TrimSpace(b.String())
}
