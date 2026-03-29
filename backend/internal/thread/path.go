package thread

import (
	"encoding/json"
	"sort"

	"github.com/mobtgzhang/clawmind/backend/internal/domain"
)

// RootKey 与前端、会话 metadata 中 siblingChoice 的根键一致。
const RootKey = "__root__"

// ParseSiblingChoice 从会话 metadata 解析分支选择（parentKey -> 选中的用户消息 id）。
func ParseSiblingChoice(meta map[string]string) map[string]string {
	out := map[string]string{}
	if meta == nil {
		return out
	}
	raw := meta["siblingChoice"]
	if raw == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// ParentKey 将消息的父 id 转为 siblingChoice 中的键。
func ParentKey(parentID *string) string {
	if parentID == nil || *parentID == "" {
		return RootKey
	}
	return *parentID
}

// LegacyLinearThread 为 true 时按创建时间展示整条会话（无任何父子链的旧数据）。
// 新模型下助手消息始终挂 user id：只要存在带 parent 的 assistant，就必须走树路径，
// 否则编辑产生的根级用户兄弟会与后续回合混在一起，看起来像「在下面新开了一段对话」。
func LegacyLinearThread(msgs []domain.Message) bool {
	for _, m := range msgs {
		if m.Role == domain.RoleAssistant && m.ParentMessageID != nil && *m.ParentMessageID != "" {
			return false
		}
		if m.Role == domain.RoleUser && m.ParentMessageID != nil && *m.ParentMessageID != "" {
			return false
		}
	}
	return true
}

func sortByCreated(msgs []domain.Message) []domain.Message {
	out := append([]domain.Message(nil), msgs...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// BuildPathForSession 按 siblingChoice 展开当前激活路径上的消息（按对话顺序）。
func BuildPathForSession(all []domain.Message, choice map[string]string) []domain.Message {
	if len(all) == 0 {
		return nil
	}
	sorted := sortByCreated(all)
	if LegacyLinearThread(sorted) {
		return sorted
	}
	children := make(map[string][]domain.Message)
	for _, m := range sorted {
		if m.Role != domain.RoleUser {
			continue
		}
		k := ParentKey(m.ParentMessageID)
		children[k] = append(children[k], m)
	}
	var out []domain.Message
	parentKey := RootKey
	for {
		users := children[parentKey]
		if len(users) == 0 {
			break
		}
		chosenID := choice[parentKey]
		var u *domain.Message
		if chosenID != "" {
			for i := range users {
				if users[i].ID == chosenID {
					u = &users[i]
					break
				}
			}
		}
		if u == nil {
			u = &users[len(users)-1]
		}
		out = append(out, *u)
		var asst *domain.Message
		for _, m := range sorted {
			if m.Role != domain.RoleAssistant || m.ParentMessageID == nil || *m.ParentMessageID != u.ID {
				continue
			}
			if asst == nil || m.CreatedAt.Before(asst.CreatedAt) {
				asst = &m
			}
		}
		if asst == nil {
			break
		}
		out = append(out, *asst)
		parentKey = asst.ID
	}
	return out
}

// PrefixThroughUser 返回路径上从开始到指定用户消息（含）的前缀，供构造 LLM 上下文。
func PrefixThroughUser(path []domain.Message, userID string) []domain.Message {
	for i, m := range path {
		if m.ID == userID {
			return append([]domain.Message(nil), path[:i+1]...)
		}
	}
	return nil
}
