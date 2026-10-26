package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestTokenizeSplitsCJKRunesAndAsciiWords(t *testing.T) {
	t.Parallel()
	tokens := Tokenize("改公共 API 前先 grep 调用点")
	joined := strings.Join(tokens, " ")
	for _, want := range []string{"api", "grep", "改", "公", "共"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tokens %q missing %q", joined, want)
		}
	}
	if len(Tokenize("")) != 0 {
		t.Fatal("empty query has no terms")
	}
}

func TestBM25RanksTheRelevantDocumentFirst(t *testing.T) {
	t.Parallel()
	docs := []string{
		"不要重构旧的鉴权模块，移动端还在使用它。",
		"今天天气不错，适合散步。",
		"鉴权模块的接口需要保持向后兼容。",
	}
	scores := BM25Scores("鉴权模块", docs)
	if len(scores) != len(docs) {
		t.Fatalf("scores = %v", scores)
	}
	if scores[1] != 0 {
		t.Fatalf("an unrelated document should score 0, got %v", scores[1])
	}
	if scores[0] <= 0 || scores[2] <= 0 {
		t.Fatalf("relevant documents should score above zero: %v", scores)
	}
	if scores[0] <= 0 || scores[2] <= 0 {
		t.Fatalf("relevant documents should score above zero: %v", scores)
	}
	if scores[2] <= scores[0] {
		t.Fatalf("the tighter matching document should rank first: %v", scores)
	}
}

func TestFuseRewardsItemsRankedByBothLists(t *testing.T) {
	t.Parallel()
	scores := Fuse([][]string{
		{"a", "b", "c"},
		{"a", "d"},
	}, RRFConstant)
	if scores["a"] <= scores["b"] || scores["a"] <= scores["d"] {
		t.Fatalf("an item ranked by both lists should win: %v", scores)
	}
	if scores["c"] >= scores["a"] || scores["d"] >= scores["a"] {
		t.Fatalf("single-list items should not outrank a fused item: %v", scores)
	}
}

func TestRetrieverAppliesItemAndCharacterBudgets(t *testing.T) {
	t.Parallel()
	var pool []Row
	for i := 0; i < 60; i++ {
		pool = append(pool, Row{Layer: LayerAtom, Kind: "atom", Content: strings.Repeat("x", 40)})
	}
	pool = append(pool, Row{Layer: LayerPersona, Kind: "persona", Content: "long-term profile line"})

	ret := NewRetriever(nil)
	ret.Budget = Budget{MaxItems: 5, CharBudget: 120, Timeout: time.Second}
	out, err := ret.Search(context.Background(), pool, "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 5 {
		t.Fatalf("item budget exceeded: %d", len(out))
	}
	used := 0
	for _, line := range out {
		used += len(line)
	}
	if used > 120 {
		t.Fatalf("character budget exceeded: %d", used)
	}
	if len(out) == 0 {
		t.Fatal("budgets should trim, not empty the result")
	}
}

func TestRetrieverBootstrapLayersComeBeforeFactLayers(t *testing.T) {
	t.Parallel()
	pool := []Row{
		{Layer: LayerAtom, Kind: "atom", Content: "鉴权模块保持向后兼容"},
		{Layer: LayerScenario, Kind: "scenario", Content: "项目当前状态：鉴权模块冻结"},
		{Layer: LayerPersona, Kind: "persona", Content: "用户偏好小步改动"},
	}
	out, err := NewRetriever(nil).Search(context.Background(), pool, "鉴权")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 2 {
		t.Fatalf("out = %v", out)
	}
	if !strings.Contains(out[0], "[L2]") && !strings.Contains(out[0], "[L3]") {
		t.Fatalf("bootstrap layers should come first: %v", out)
	}
}

func TestPipelineDistillsLayersWithoutAModel(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	pipeline := NewPipeline(store, nil, DefaultDistillConfig())
	text := "用户决定不要重构旧的鉴权模块，因为移动端还在使用它。\n我们选择先写失败测试再实现。"
	if err := pipeline.Observe(context.Background(), "s1", "p1", text); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Rows(context.Background(), "s1", "p1", 100)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[Layer]int{}
	for _, row := range rows {
		counts[row.Layer]++
	}
	if counts[LayerConversation] != 1 {
		t.Fatalf("L0 should hold the raw turn: %v", counts)
	}
	if counts[LayerAtom] == 0 {
		t.Fatalf("L1 atoms were not extracted: %v", counts)
	}
	if counts[LayerScenario] == 0 {
		t.Fatalf("L2 scenario was not built: %v", counts)
	}
	if counts[LayerPersona] == 0 {
		t.Fatalf("L3 persona was not built: %v", counts)
	}
}

func TestPipelineMergesPersonaInsteadOfAppending(t *testing.T) {
	t.Parallel()
	store := NewInMemoryStore()
	pipeline := NewPipeline(store, nil, DefaultDistillConfig())
	ctx := context.Background()
	text := "用户偏好小步改动，必须保持向后兼容。"
	if err := pipeline.Run(ctx, "s1", "p1", text); err != nil {
		t.Fatal(err)
	}
	if err := pipeline.Run(ctx, "s1", "p1", "用户再次强调必须保持向后兼容。"); err != nil {
		t.Fatal(err)
	}
	rows, _ := store.Rows(ctx, "s1", "p1", 100)
	var persona []Row
	for _, row := range rows {
		if row.Layer == LayerPersona {
			persona = append(persona, row)
		}
	}
	if len(persona) < 2 {
		t.Fatalf("persona rows = %d", len(persona))
	}
	if !strings.Contains(persona[len(persona)-1].Content, "向后兼容") {
		t.Fatalf("the merged profile should keep the pattern that repeated: %q", persona[len(persona)-1].Content)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", t.TempDir()+"/memory.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := NewSQLiteStore(db)
	if err := store.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSQLiteStoreKeepsL0SeparateFromL1(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ctx := context.Background()
	store := NewSQLiteStore(db)
	if err := store.AppendLevel(ctx, "s1", "p1", int(LayerConversation), "conversation", "原始对话内容"); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendLevel(ctx, "s1", "p1", int(LayerAtom), "atom", "鉴权模块保持向后兼容"); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Rows(ctx, "s1", "p1", 100)
	if err != nil {
		t.Fatal(err)
	}
	layers := map[Layer]int{}
	for _, row := range rows {
		layers[row.Layer]++
	}
	if layers[LayerConversation] != 1 || layers[LayerAtom] != 1 {
		t.Fatalf("layers = %v", layers)
	}
	out, err := store.RetrieveLevels(ctx, "s1", "p1", "鉴权")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 || !strings.Contains(out[0], "[L1]") {
		t.Fatalf("retrieve = %v", out)
	}
}

func TestAssetRegistryAccessAndLoadout(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ctx := context.Background()
	reg := NewAssetRegistry(db)
	if err := reg.EnsureSchema(); err != nil {
		t.Fatal(err)
	}

	owner := Viewer{UserID: "u1", TeamID: "t1", AgentID: "a1", Role: "member"}
	other := Viewer{UserID: "u2", TeamID: "t1", AgentID: "a2", Role: "admin"}

	private := &Asset{Type: AssetChatMemory, Name: "private note", OwnerID: "u1", TeamID: "t1", Visibility: VisibilityPrivate, Status: StatusReady}
	if err := reg.Register(ctx, private); err != nil {
		t.Fatal(err)
	}
	if ok, err := reg.CanAccess(ctx, private.ID, other); err != nil || ok {
		t.Fatalf("a private asset belongs strictly to its owner; team admin got %v (%v)", ok, err)
	}

	team := &Asset{Type: AssetSkill, Name: "release skill", OwnerID: "u1", TeamID: "t1", Visibility: VisibilityTeam, Status: StatusReady}
	if err := reg.Register(ctx, team); err != nil {
		t.Fatal(err)
	}
	if ok, err := reg.CanAccess(ctx, team.ID, other); err != nil || !ok {
		t.Fatalf("team visibility should allow a team member: %v (%v)", ok, err)
	}

	restricted := &Asset{Type: AssetWiki, Name: "restricted wiki", OwnerID: "u1", TeamID: "t1", Visibility: VisibilityRestricted, Status: StatusReady}
	if err := reg.Register(ctx, restricted); err != nil {
		t.Fatal(err)
	}
	if ok, err := reg.CanAccess(ctx, restricted.ID, other); err != nil || ok {
		t.Fatalf("restricted needs a precise grant: %v (%v)", ok, err)
	}
	if err := reg.AddACL(ctx, restricted.ID, "agent", "a2"); err != nil {
		t.Fatal(err)
	}
	if ok, err := reg.CanAccess(ctx, restricted.ID, other); err != nil || !ok {
		t.Fatalf("the grant should allow the named agent: %v (%v)", ok, err)
	}
	if err := reg.Revoke(ctx, restricted.ID, "agent", "a2"); err != nil {
		t.Fatal(err)
	}
	if ok, err := reg.CanAccess(ctx, restricted.ID, other); err != nil || ok {
		t.Fatalf("revoking a grant must revoke access: %v (%v)", ok, err)
	}

	// An asset that is not ready yet is not usable, even for its owner.
	pending := &Asset{Type: AssetCodeGraph, Name: "code graph", OwnerID: "u1", TeamID: "t1", Visibility: VisibilityAgent, Status: StatusDraft}
	if err := reg.Register(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if ok, err := reg.CanAccess(ctx, pending.ID, owner); err != nil || !ok {
		t.Fatalf("the owner can always read their own asset: %v (%v)", ok, err)
	}
	if err := reg.SetStatus(ctx, pending.ID, StatusArchived); err != nil {
		t.Fatal(err)
	}
	if ok, err := reg.CanAccess(ctx, pending.ID, owner); err != nil || ok {
		t.Fatalf("an archived asset is not usable: %v (%v)", ok, err)
	}

	if _, err := reg.Bind(ctx, team.ID, "a1"); err != nil {
		t.Fatal(err)
	}
	loadout, err := reg.Loadout(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadout) != 0 {
		t.Fatalf("a binding for another agent must not appear in this loadout: %v", loadout)
	}
	loadout, err = reg.Loadout(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadout) != 1 || loadout[0].Name != "release skill" {
		t.Fatalf("loadout = %v", loadout)
	}
}

func TestInMemoryStoreTrimsToTheCapTheLayerSet(t *testing.T) {
	store := NewInMemoryStore()
	store.Cap = LayerCap{L1: 2}
	for i := 0; i < 5; i++ {
		if err := store.AppendLevel(context.Background(), "s1", "", int(LayerAtom), "fact", fmt.Sprintf("fact %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := store.Rows(context.Background(), "s1", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want the cap the memory layer lowered to be enforced, not only reported", len(rows))
	}
}
