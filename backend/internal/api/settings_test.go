package api

import (
	"testing"

	"github.com/mobtgzhang/clawmind/backend/internal/memory"
	"github.com/mobtgzhang/clawmind/backend/internal/plugin"
)

func TestLayerCapReportsTheCapTheRuntimeEnforces(t *testing.T) {
	s := &Server{}
	if s.layerCap() != memory.DefaultLayerCap() {
		t.Fatal("an unset cap must fall back to the defaults the census compares against")
	}
	s.Cap = memory.LayerCap{L0: 120, L1: 60, L2: 40, L3: 80}
	if got := s.layerCap(); got.L0 != 120 || got.L1 != 60 {
		t.Fatalf("layerCap = %+v, want the cap the memory layer lowered", got)
	}
}

func TestSettingsSourceNamesPluginsUnderTheNamesTheUIReads(t *testing.T) {
	reg := plugin.NewRegistry(t.TempDir())
	manifest := &plugin.Manifest{ID: "prod", Enabled: true, Seams: []plugin.Seam{plugin.SeamLLM}, Patch: []plugin.PatchRow{
		{ID: "model", Seam: plugin.SeamLLM, Config: map[string]any{"model": "deepseek-chat", "temperature": 0.2}},
	}}
	if err := reg.Mount(manifest); err != nil {
		t.Fatal(err)
	}
	source := (&Server{Plugins: reg}).settingsSource()
	if source["openaiModel"] != "prod" {
		t.Fatalf("source = %+v, want the patch key to be reported under the name the settings page reads", source)
	}
	if source["temperature"] != "prod" {
		t.Fatalf("source = %+v", source)
	}
	if (&Server{}).settingsSource() != nil {
		t.Fatal("a server with no plugin registry must report no source")
	}
}
