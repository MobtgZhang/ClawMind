package memory

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Asset types are registered uniformly: chat memory, skills, wiki pages and code graphs are all
// memory assets, so ownership, versioning and access control exist once instead of per feature.
const (
	AssetChatMemory = "chat_memory"
	AssetSkill      = "skill"
	AssetWiki       = "wiki"
	AssetCodeGraph  = "codegraph"
)

func AssetTypes() []string {
	return []string{AssetChatMemory, AssetSkill, AssetWiki, AssetCodeGraph}
}

// Visibility semantics. Sharing is an explicit action, not a default leak.
const (
	VisibilityPrivate    = "private"
	VisibilityTeam       = "team"
	VisibilityRestricted = "restricted"
	VisibilityAgent      = "agent"
)

func Visibilities() []string {
	return []string{VisibilityPrivate, VisibilityTeam, VisibilityRestricted, VisibilityAgent}
}

func ValidVisibility(v string) bool {
	for _, known := range Visibilities() {
		if v == known {
			return true
		}
	}
	return false
}

// Status tracks the async build pipeline: wiki and code graph assets are not usable until ready.
const (
	StatusDraft    = "draft"
	StatusReady    = "ready"
	StatusArchived = "archived"
)

func ValidStatus(s string) bool {
	return s == StatusDraft || s == StatusReady || s == StatusArchived
}

// ACLRule is one precise grant for a restricted asset.
type ACLRule struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // user | role | agent
	Target string `json:"target"`
}

// Asset is one registered memory asset.
type Asset struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Name       string    `json:"name"`
	OwnerID    string    `json:"ownerId"`
	TeamID     string    `json:"teamId"`
	Version    string    `json:"version"`
	Status     string    `json:"status"`
	Visibility string    `json:"visibility"`
	Content    string    `json:"content"`
	UsageCount int       `json:"usageCount"`
	CreatedAt  int64     `json:"createdAt"`
	UpdatedAt  int64     `json:"updatedAt"`
	ACL        []ACLRule `json:"acl,omitempty"`
}

// Viewer is who is asking. Access is decided before retrieval, not after.
type Viewer struct {
	UserID  string
	TeamID  string
	AgentID string
	Role    string // admin | member
}

// Binding is a fixed equip of an asset onto an agent.
type Binding struct {
	ID        string `json:"id"`
	AssetID   string `json:"assetId"`
	AgentID   string `json:"agentId"`
	Mode      string `json:"mode"`
	CreatedAt int64  `json:"createdAt"`
}

// AssetRegistry is the memory asset registry: ownership, version, status, visibility, usage and
// agent bindings in one place.
type AssetRegistry struct {
	db *sql.DB
}

func NewAssetRegistry(db *sql.DB) *AssetRegistry { return &AssetRegistry{db: db} }

// EnsureSchema creates the asset, ACL and binding tables.
func (r *AssetRegistry) EnsureSchema() error {
	if r.db == nil {
		return fmt.Errorf("memory: no database")
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS memory_assets (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  name TEXT NOT NULL,
  owner_id TEXT NOT NULL DEFAULT '',
  team_id TEXT NOT NULL DEFAULT '',
  version TEXT NOT NULL DEFAULT '1',
  status TEXT NOT NULL DEFAULT 'draft',
  visibility TEXT NOT NULL DEFAULT 'private',
  content TEXT NOT NULL DEFAULT '',
  usage_count INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);`,
		`CREATE TABLE IF NOT EXISTS memory_asset_acl (
  id TEXT PRIMARY KEY,
  asset_id TEXT NOT NULL REFERENCES memory_assets(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  target TEXT NOT NULL
);`,
		`CREATE INDEX IF NOT EXISTS idx_memory_asset_acl_asset ON memory_asset_acl(asset_id, kind, target);`,
		`CREATE TABLE IF NOT EXISTS memory_asset_bindings (
  id TEXT PRIMARY KEY,
  asset_id TEXT NOT NULL REFERENCES memory_assets(id) ON DELETE CASCADE,
  agent_id TEXT NOT NULL,
  mode TEXT NOT NULL DEFAULT 'fixed',
  created_at INTEGER NOT NULL
);`,
		`CREATE INDEX IF NOT EXISTS idx_memory_asset_bindings_agent ON memory_asset_bindings(agent_id, created_at);`,
	}
	for _, q := range stmts {
		if _, err := r.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// Register inserts an asset. New assets are private by default.
func (r *AssetRegistry) Register(ctx context.Context, a *Asset) error {
	if a == nil {
		return fmt.Errorf("memory: no asset")
	}
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("memory: asset needs a name")
	}
	if !ValidVisibility(a.Visibility) {
		return fmt.Errorf("memory: unknown visibility %q; use %s", a.Visibility, strings.Join(Visibilities(), ", "))
	}
	if a.Status == "" {
		a.Status = StatusDraft
	}
	if !ValidStatus(a.Status) {
		return fmt.Errorf("memory: unknown status %q", a.Status)
	}
	if a.Version == "" {
		a.Version = "1"
	}
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	now := time.Now().UnixMilli()
	a.CreatedAt, a.UpdatedAt = now, now
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO memory_assets (id, type, name, owner_id, team_id, version, status, visibility, content, usage_count, created_at, updated_at)
 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Type, a.Name, a.OwnerID, a.TeamID, a.Version, a.Status, a.Visibility, a.Content, a.UsageCount, a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return err
	}
	for _, rule := range a.ACL {
		if err := r.AddACL(ctx, a.ID, rule.Kind, rule.Target); err != nil {
			return err
		}
	}
	return nil
}

// Get reads one asset with its ACL.
func (r *AssetRegistry) Get(ctx context.Context, id string) (*Asset, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, type, name, owner_id, team_id, version, status, visibility, content, usage_count, created_at, updated_at
 FROM memory_assets WHERE id=?`, id)
	var a Asset
	if err := row.Scan(&a.ID, &a.Type, &a.Name, &a.OwnerID, &a.TeamID, &a.Version, &a.Status, &a.Visibility, &a.Content, &a.UsageCount, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	a.ACL = r.acl(ctx, a.ID)
	return &a, nil
}

func (r *AssetRegistry) acl(ctx context.Context, assetID string) []ACLRule {
	rows, err := r.db.QueryContext(ctx, `SELECT id, kind, target FROM memory_asset_acl WHERE asset_id=? ORDER BY kind, target`, assetID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []ACLRule
	for rows.Next() {
		var rule ACLRule
		if err := rows.Scan(&rule.ID, &rule.Kind, &rule.Target); err != nil {
			continue
		}
		out = append(out, rule)
	}
	return out
}

// CanAccess answers "who can use it". A private asset is strictly the owner's, not even team
// admins; a restricted asset needs a matching grant.
func (r *AssetRegistry) CanAccess(ctx context.Context, assetID string, viewer Viewer) (bool, error) {
	a, err := r.Get(ctx, assetID)
	if err != nil {
		return false, err
	}
	return canAccessAsset(a, viewer), nil
}

func canAccessAsset(a *Asset, viewer Viewer) bool {
	if a.Status == StatusArchived {
		return false
	}
	if a.OwnerID != "" && a.OwnerID == viewer.UserID {
		return true
	}
	switch a.Visibility {
	case VisibilityPrivate:
		return false
	case VisibilityTeam:
		return a.TeamID != "" && a.TeamID == viewer.TeamID
	case VisibilityRestricted:
		for _, rule := range a.ACL {
			switch rule.Kind {
			case "user":
				if rule.Target == viewer.UserID {
					return true
				}
			case "role":
				if rule.Target == viewer.Role {
					return true
				}
			case "agent":
				if rule.Target == viewer.AgentID {
					return true
				}
			}
		}
		return false
	case VisibilityAgent:
		return a.TeamID != "" && a.TeamID == viewer.TeamID
	}
	return false
}

// List returns the assets a viewer can use, narrowed by permission first and then by type.
func (r *AssetRegistry) List(ctx context.Context, viewer Viewer, assetType string) ([]Asset, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, type, name, owner_id, team_id, version, status, visibility, content, usage_count, created_at, updated_at
 FROM memory_assets ORDER BY updated_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Asset
	for rows.Next() {
		var a Asset
		if err := rows.Scan(&a.ID, &a.Type, &a.Name, &a.OwnerID, &a.TeamID, &a.Version, &a.Status, &a.Visibility, &a.Content, &a.UsageCount, &a.CreatedAt, &a.UpdatedAt); err != nil {
			continue
		}
		if assetType != "" && a.Type != assetType {
			continue
		}
		if canAccessAsset(&a, viewer) {
			out = append(out, a)
		}
	}
	return out, rows.Err()
}

// Share changes visibility. Revoking sharing is an explicit action.
func (r *AssetRegistry) Share(ctx context.Context, assetID, visibility string) error {
	if !ValidVisibility(visibility) {
		return fmt.Errorf("memory: unknown visibility %q", visibility)
	}
	_, err := r.db.ExecContext(ctx, `UPDATE memory_assets SET visibility=?, updated_at=? WHERE id=?`, visibility, time.Now().UnixMilli(), assetID)
	return err
}

// SetStatus moves an asset through the async build pipeline.
func (r *AssetRegistry) SetStatus(ctx context.Context, assetID, status string) error {
	if !ValidStatus(status) {
		return fmt.Errorf("memory: unknown status %q", status)
	}
	_, err := r.db.ExecContext(ctx, `UPDATE memory_assets SET status=?, updated_at=? WHERE id=?`, status, time.Now().UnixMilli(), assetID)
	return err
}

// AddACL grants access to one user, role or agent.
func (r *AssetRegistry) AddACL(ctx context.Context, assetID, kind, target string) error {
	if kind != "user" && kind != "role" && kind != "agent" {
		return fmt.Errorf("memory: acl kind must be user, role or agent")
	}
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("memory: acl needs a target")
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO memory_asset_acl (id, asset_id, kind, target) VALUES (?,?,?,?)`,
		uuid.NewString(), assetID, kind, strings.TrimSpace(target))
	return err
}

// Revoke removes one grant.
func (r *AssetRegistry) Revoke(ctx context.Context, assetID, kind, target string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM memory_asset_acl WHERE asset_id=? AND kind=? AND target=?`, assetID, kind, strings.TrimSpace(target))
	return err
}

// Bind equips an asset onto an agent. Re-equipping an agent on a new framework is a binding
// change, not a retraining.
func (r *AssetRegistry) Bind(ctx context.Context, assetID, agentID string) (string, error) {
	if strings.TrimSpace(agentID) == "" {
		return "", fmt.Errorf("memory: binding needs an agent")
	}
	if _, err := r.Get(ctx, assetID); err != nil {
		return "", err
	}
	id := uuid.NewString()
	_, err := r.db.ExecContext(ctx, `INSERT INTO memory_asset_bindings (id, asset_id, agent_id, mode, created_at) VALUES (?,?,?,?,?)`,
		id, assetID, agentID, "fixed", time.Now().UnixMilli())
	if err != nil {
		return "", err
	}
	return id, nil
}

// Unbind removes an equip.
func (r *AssetRegistry) Unbind(ctx context.Context, bindingID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM memory_asset_bindings WHERE id=?`, bindingID)
	return err
}

// Loadout is what one agent actually receives: fixed bindings, filtered by permission first.
func (r *AssetRegistry) Loadout(ctx context.Context, viewer Viewer) ([]Asset, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT b.id, b.asset_id, b.agent_id, b.mode, b.created_at
 FROM memory_asset_bindings b ORDER BY b.created_at ASC LIMIT 200`,
	)
	if err != nil {
		return nil, err
	}
	var bindings []Binding
	for rows.Next() {
		var b Binding
		if err := rows.Scan(&b.ID, &b.AssetID, &b.AgentID, &b.Mode, &b.CreatedAt); err != nil {
			continue
		}
		if viewer.AgentID == "" || b.AgentID == viewer.AgentID {
			bindings = append(bindings, b)
		}
	}
	rows.Close()

	var out []Asset
	for _, b := range bindings {
		a, err := r.Get(ctx, b.AssetID)
		if err != nil {
			continue
		}
		if canAccessAsset(a, viewer) {
			out = append(out, *a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// TouchUsage records that an asset entered context, which is how "has it been used recently"
// becomes answerable.
func (r *AssetRegistry) TouchUsage(ctx context.Context, assetID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE memory_assets SET usage_count = usage_count + 1 WHERE id=?`, assetID)
	return err
}
