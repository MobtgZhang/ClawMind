// Package mcpclient implements a minimal MCP client over stdio (JSON-RPC newline-delimited).
package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mobtgzhang/clawmind/backend/internal/tools"
)

// Session is a connected MCP subprocess with cached tool definitions.
type Session struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	mu    sync.Mutex
	nextID int64
	pend  map[string]chan rpcEnvelope
	tools []tools.Definition
}

type rpcEnvelope struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

// Connect starts command with args and optional extra env (KEY=VAL), runs initialize + tools/list.
func Connect(ctx context.Context, command string, args []string, env []string) (*Session, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, errors.New("empty mcp command")
	}
	cmd := exec.CommandContext(ctx, command, args...)
	if len(env) > 0 {
		cmd.Env = append(append([]string{}, cmd.Environ()...), env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &Session{
		cmd:   cmd,
		stdin: stdin,
		pend:  make(map[string]chan rpcEnvelope),
	}
	go s.readLoop(stdout)
	if err := s.handshake(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	tdefs, err := s.fetchTools(ctx)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	s.tools = tdefs
	return s, nil
}

func rpcIDKey(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return string(n)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

func (s *Session) readLoop(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var wrap struct {
			ID json.RawMessage `json:"id"`
			rpcEnvelope
		}
		if err := json.Unmarshal([]byte(line), &wrap); err != nil {
			continue
		}
		idKey := rpcIDKey(wrap.ID)
		if idKey == "" {
			continue
		}
		s.mu.Lock()
		ch := s.pend[idKey]
		delete(s.pend, idKey)
		s.mu.Unlock()
		if ch != nil {
			ch <- wrap.rpcEnvelope
			close(ch)
		}
	}
}

func (s *Session) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := atomic.AddInt64(&s.nextID, 1)
	idKey := fmt.Sprintf("%d", id)
	ch := make(chan rpcEnvelope, 1)
	s.mu.Lock()
	s.pend[idKey] = ch
	s.mu.Unlock()
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	b, err := json.Marshal(req)
	if err != nil {
		s.mu.Lock()
		delete(s.pend, idKey)
		s.mu.Unlock()
		return nil, err
	}
	line := append(b, '\n')
	s.mu.Lock()
	_, werr := s.stdin.Write(line)
	s.mu.Unlock()
	if werr != nil {
		return nil, werr
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case ev, ok := <-ch:
		if !ok {
			return nil, errors.New("mcp closed")
		}
		if ev.Error != nil {
			return nil, fmt.Errorf("mcp error: %s", ev.Error.Message)
		}
		return ev.Result, nil
	}
}

func (s *Session) handshake(ctx context.Context) error {
	_, err := s.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "clawmind",
			"version": "1.0.0",
		},
	})
	if err != nil {
		return err
	}
	n := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	}
	b, _ := json.Marshal(n)
	s.mu.Lock()
	_, err = s.stdin.Write(append(b, '\n'))
	s.mu.Unlock()
	return err
}

func (s *Session) fetchTools(ctx context.Context) ([]tools.Definition, error) {
	raw, err := s.call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	var out []tools.Definition
	for _, t := range parsed.Tools {
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		params := t.InputSchema
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, tools.Definition{
			Type: "function",
			Function: tools.FunctionSpec{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return out, nil
}

// Definitions returns cached OpenAI-style tool definitions from the MCP server.
func (s *Session) Definitions() []tools.Definition {
	return s.tools
}

// CallTool invokes tools/call on the MCP server.
func (s *Session) CallTool(ctx context.Context, name string, argsJSON string) (string, error) {
	var args any = map[string]any{}
	if t := strings.TrimSpace(argsJSON); t != "" && t != "null" {
		if err := json.Unmarshal([]byte(t), &args); err != nil {
			args = map[string]any{"_raw": t}
		}
	}
	raw, err := s.call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return "", err
	}
	var tr struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return string(raw), nil
	}
	var b strings.Builder
	for _, c := range tr.Content {
		if c.Text != "" {
			b.WriteString(c.Text)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		out = string(raw)
	}
	if tr.IsError {
		return "", fmt.Errorf("%s", out)
	}
	return out, nil
}

// Close terminates the subprocess.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	if s.cmd != nil {
		_ = s.cmd.Wait()
	}
	return nil
}
