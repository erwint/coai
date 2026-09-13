package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type joinArgs struct {
	Session string `json:"session"`
	Agent   string `json:"agent"`
	Kind    string `json:"kind"`
	CWD     string `json:"cwd"`
}
type messageArgs struct {
	Session string `json:"session"`
	From    string `json:"from"`
	To      string `json:"to"`
	Text    string `json:"text"`
}
type taskArgs struct {
	Session     string `json:"session"`
	From        string `json:"from"`
	To          string `json:"to"`
	Description string `json:"description"`
}
type taskIDArgs struct {
	TaskID string `json:"task_id"`
}
type messageIDArgs struct {
	MessageID string `json:"message_id"`
}
type inboxArgs struct {
	Session string `json:"session"`
	Agent   string `json:"agent"`
}
type completeArgs struct {
	TaskID string `json:"task_id"`
	Result string `json:"result"`
}
type listArgs struct {
	Session string `json:"session"`
}
type threadArgs struct {
	Session  string `json:"session"`
	Agent    string `json:"agent"`
	ThreadID string `json:"thread_id"`
	Socket   string `json:"socket,omitempty"`
}
type leaseArgs struct {
	Session    string `json:"session"`
	Agent      string `json:"agent"`
	Path       string `json:"path"`
	TTLSeconds int    `json:"ttl_seconds"`
}

func text(v any) (*mcp.CallToolResult, any, error) {
	b, _ := json.Marshal(v)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
}
func main() {
	defaultPath := os.Getenv("COAI_STATE")
	if defaultPath == "" {
		d, _ := os.UserConfigDir()
		defaultPath = filepath.Join(d, "coai", "state.json")
	}
	path := flag.String("state", defaultPath, "shared state file")
	flag.Parse()
	b := NewBroker(*path)
	var localMu sync.RWMutex
	localAgent := ""
	// Claude Code recognizes this experimental capability and accepts the
	// custom channel notification below. Other MCP clients safely ignore it.
	s := mcp.NewServer(&mcp.Implementation{Name: "coai-coordinator", Version: "0.1.0"}, &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{Experimental: map[string]any{"claude/channel": map[string]any{}}, Tools: &mcp.ToolCapabilities{}}})
	mcp.AddTool(s, &mcp.Tool{Name: "join_session", Description: "Register an agent in a shared coordination session."}, func(_ context.Context, _ *mcp.CallToolRequest, a joinArgs) (*mcp.CallToolResult, any, error) {
		if a.Agent == "" || a.Session == "" {
			return nil, nil, fmt.Errorf("session and agent are required")
		}
		if a.CWD == "" {
			a.CWD, _ = os.Getwd()
		}
		localMu.Lock()
		localAgent = a.Agent
		localMu.Unlock()
		err := b.Join(Agent{ID: a.Agent, Session: a.Session, Kind: a.Kind, CWD: a.CWD})
		return text(map[string]any{"joined": err == nil, "session": a.Session, "agent": a.Agent})
	})
	go func() {
		seen := map[string]bool{}
		t := time.NewTicker(300 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			localMu.RLock()
			agent := localAgent
			localMu.RUnlock()
			if agent == "" {
				continue
			}
			st, err := b.Snapshot("")
			if err != nil {
				continue
			}
			for _, msg := range st.Messages {
				if msg.To != agent || seen[msg.ID] {
					continue
				}
				seen[msg.ID] = true
				for session := range s.Sessions() {
					_ = session.SendNotification(context.Background(), "notifications/claude/channel", map[string]any{"content": msg.Text, "meta": map[string]string{"session": msg.Session, "from": msg.From, "message_id": msg.ID}})
				}
			}
		}
	}()
	mcp.AddTool(s, &mcp.Tool{Name: "send_message", Description: "Send a durable message to another agent."}, func(_ context.Context, _ *mcp.CallToolRequest, a messageArgs) (*mcp.CallToolResult, any, error) {
		err := b.Send(Message{Session: a.Session, From: a.From, To: a.To, Text: a.Text})
		return text(map[string]any{"sent": err == nil})
	})
	mcp.AddTool(s, &mcp.Tool{Name: "ack_message", Description: "Acknowledge durable delivery of a message."}, func(_ context.Context, _ *mcp.CallToolRequest, a messageIDArgs) (*mcp.CallToolResult, any, error) {
		e := b.Ack(a.MessageID)
		return text(map[string]any{"acknowledged": e == nil, "error": errString(e)})
	})
	mcp.AddTool(s, &mcp.Tool{Name: "claim_files", Description: "Claim a file path for exclusive advisory editing in this session."}, func(_ context.Context, _ *mcp.CallToolRequest, a leaseArgs) (*mcp.CallToolResult, any, error) {
		if a.TTLSeconds <= 0 {
			a.TTLSeconds = 900
		}
		e := b.Claim(a.Session, a.Agent, a.Path, time.Duration(a.TTLSeconds)*time.Second)
		return text(map[string]any{"claimed": e == nil, "error": errString(e)})
	})
	mcp.AddTool(s, &mcp.Tool{Name: "release_files", Description: "Release a previously claimed file path."}, func(_ context.Context, _ *mcp.CallToolRequest, a leaseArgs) (*mcp.CallToolResult, any, error) {
		e := b.Release(a.Session, a.Agent, a.Path)
		return text(map[string]any{"released": e == nil, "error": errString(e)})
	})
	mcp.AddTool(s, &mcp.Tool{Name: "renew_files", Description: "Renew an advisory file lease."}, func(_ context.Context, _ *mcp.CallToolRequest, a leaseArgs) (*mcp.CallToolResult, any, error) {
		if a.TTLSeconds <= 0 {
			a.TTLSeconds = 900
		}
		e := b.Renew(a.Session, a.Agent, a.Path, time.Duration(a.TTLSeconds)*time.Second)
		return text(map[string]any{"renewed": e == nil, "error": errString(e)})
	})
	mcp.AddTool(s, &mcp.Tool{Name: "inbox", Description: "List unacknowledged messages for an agent."}, func(_ context.Context, _ *mcp.CallToolRequest, a inboxArgs) (*mcp.CallToolResult, any, error) {
		v, e := b.Messages(a.Session, a.Agent)
		return text(map[string]any{"messages": v, "error": errString(e)})
	})
	mcp.AddTool(s, &mcp.Tool{Name: "register_codex_thread", Description: "Associate a Codex app-server thread and Unix socket with an agent."}, func(_ context.Context, _ *mcp.CallToolRequest, a threadArgs) (*mcp.CallToolResult, any, error) {
		err := b.RegisterThread(a.Session, a.Agent, a.ThreadID, a.Socket)
		return text(map[string]any{"registered": err == nil, "error": errString(err)})
	})
	mcp.AddTool(s, &mcp.Tool{Name: "create_task", Description: "Create work for another agent and return its task ID."}, func(_ context.Context, _ *mcp.CallToolRequest, a taskArgs) (*mcp.CallToolResult, any, error) {
		id, err := b.Start(Task{Session: a.Session, From: a.From, To: a.To, Description: a.Description})
		return text(map[string]any{"task_id": id, "created": err == nil})
	})
	mcp.AddTool(s, &mcp.Tool{Name: "complete_task", Description: "Publish a task result and inject it into the recipient's Codex thread when registered."}, func(ctx context.Context, _ *mcp.CallToolRequest, a completeArgs) (*mcp.CallToolResult, any, error) {
		err := b.Complete(a.TaskID, a.Result)
		if err == nil {
			st, _ := b.Snapshot("")
			for _, t := range st.Tasks {
				if t.ID == a.TaskID {
					for _, ag := range st.Agents {
						if ag.Session == t.Session && ag.ID == t.To && ag.ThreadID != "" {
							_ = injectCodex(ctx, ag.Socket, ag.ThreadID, fmt.Sprintf("Task %s completed by %s:\n%s", t.ID, t.From, a.Result))
						}
					}
				}
			}
		}
		return text(map[string]any{"completed": err == nil})
	})
	mcp.AddTool(s, &mcp.Tool{Name: "claim_task", Description: "Mark a pending task as running."}, func(_ context.Context, _ *mcp.CallToolRequest, a taskIDArgs) (*mcp.CallToolResult, any, error) {
		e := b.ClaimTask(a.TaskID)
		return text(map[string]any{"claimed": e == nil, "error": errString(e)})
	})
	mcp.AddTool(s, &mcp.Tool{Name: "fail_task", Description: "Mark a task failed."}, func(_ context.Context, _ *mcp.CallToolRequest, a completeArgs) (*mcp.CallToolResult, any, error) {
		e := b.SetTaskStatus(a.TaskID, "failed", a.Result)
		return text(map[string]any{"failed": e == nil, "error": errString(e)})
	})
	mcp.AddTool(s, &mcp.Tool{Name: "cancel_task", Description: "Cancel a task."}, func(_ context.Context, _ *mcp.CallToolRequest, a taskIDArgs) (*mcp.CallToolResult, any, error) {
		e := b.SetTaskStatus(a.TaskID, "cancelled", "")
		return text(map[string]any{"cancelled": e == nil, "error": errString(e)})
	})
	mcp.AddTool(s, &mcp.Tool{Name: "await_task", Description: "Block until a task completes, then return its result. The client may receive progress notifications while waiting."}, func(ctx context.Context, req *mcp.CallToolRequest, a taskIDArgs) (*mcp.CallToolResult, any, error) {
		// MCP clients that provide a progress token (including Codex app-server
		// integrations) receive a heartbeat while the durable wait is active.
		if token := req.Params.GetProgressToken(); token != nil {
			_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{ProgressToken: token, Progress: 0, Message: "waiting for task result"})
		}
		t, err := b.Await(ctx, a.TaskID)
		return text(tOrErr(t, err))
	})
	mcp.AddTool(s, &mcp.Tool{Name: "list_session", Description: "List agents, messages, and tasks in a session."}, func(_ context.Context, _ *mcp.CallToolRequest, a listArgs) (*mcp.CallToolResult, any, error) {
		v, err := b.Snapshot(a.Session)
		return text(map[string]any{"state": v, "error": errString(err)})
	})
	if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func tOrErr(t Task, e error) any {
	if e != nil {
		return map[string]string{"error": e.Error()}
	}
	return t
}
func errString(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}
