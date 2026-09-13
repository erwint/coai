package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"nhooyr.io/websocket"
	"os"
	"path/filepath"
	"time"
)

func injectCodex(ctx context.Context, socket, thread, message string) error {
	if thread == "" {
		return nil
	}
	if socket == "" {
		home := os.Getenv("CODEX_HOME")
		if home == "" {
			home, _ = os.UserHomeDir()
			home = filepath.Join(home, ".codex")
		}
		socket = filepath.Join(home, "app-server-control", "app-server-control.sock")
	}
	d := &net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{DialContext: func(c context.Context, _, _ string) (net.Conn, error) { return d.DialContext(c, "unix", socket) }}
	c, _, err := websocket.Dial(ctx, "ws://localhost", &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}})
	if err != nil {
		return err
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	send := func(id int, method string, params any) error {
		b, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
		return c.Write(ctx, websocket.MessageText, b)
	}
	if err := send(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "coai-coordinator", "version": "0.1.0"}}); err != nil {
		return err
	}
	if _, _, err = c.Read(ctx); err != nil {
		return err
	}
	if err := send(2, "thread/inject_items", map[string]any{"threadId": thread, "items": []map[string]any{{"type": "text", "text": message}}}); err != nil {
		return err
	}
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return err
		}
		var v map[string]any
		_ = json.Unmarshal(data, &v)
		if fmt.Sprint(v["id"]) == "2" {
			if v["error"] != nil {
				return fmt.Errorf("codex inject: %v", v["error"])
			}
			return nil
		}
	}
}
