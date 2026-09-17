package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
)

func TestSupabaseStore_EndToEnd(t *testing.T) {
	var mu sync.Mutex
	sessions := make(map[string]*SupabaseSessionRow)
	messages := make([]SupabaseMessageRow, 0)
	var nextMsgID int64 = 1

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		path := r.URL.Path
		query := r.URL.Query()

		switch {
		case strings.HasPrefix(path, "/rest/v1/picoclaw_sessions"):
			if r.Method == http.MethodPost {
				var row SupabaseSessionRow
				if err := json.NewDecoder(r.Body).Decode(&row); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				if existing, ok := sessions[row.SessionKey]; ok {
					if row.Summary != "" {
						existing.Summary = row.Summary
					}
					if len(row.Scope) > 0 {
						existing.Scope = row.Scope
					}
					if len(row.Aliases) > 0 {
						existing.Aliases = row.Aliases
					}
				} else {
					sessions[row.SessionKey] = &row
				}
				w.WriteHeader(http.StatusCreated)
				return
			} else if r.Method == http.MethodGet {
				sessionKeyEq := query.Get("session_key")
				if strings.HasPrefix(sessionKeyEq, "eq.") {
					key := strings.TrimPrefix(sessionKeyEq, "eq.")
					if s, ok := sessions[key]; ok {
						_ = json.NewEncoder(w).Encode([]*SupabaseSessionRow{s})
						return
					}
					_ = json.NewEncoder(w).Encode([]*SupabaseSessionRow{})
					return
				}
				// List sessions
				res := make([]*SupabaseSessionRow, 0, len(sessions))
				for _, s := range sessions {
					res = append(res, s)
				}
				_ = json.NewEncoder(w).Encode(res)
				return
			}

		case strings.HasPrefix(path, "/rest/v1/picoclaw_messages"):
			if r.Method == http.MethodPost {
				// Can be single object or array
				var raw json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}

				var single SupabaseMessageRow
				if err := json.Unmarshal(raw, &single); err == nil && single.SessionKey != "" {
					single.ID = nextMsgID
					nextMsgID++
					messages = append(messages, single)
					w.WriteHeader(http.StatusCreated)
					return
				}

				var multiple []SupabaseMessageRow
				if err := json.Unmarshal(raw, &multiple); err == nil {
					for i := range multiple {
						multiple[i].ID = nextMsgID
						nextMsgID++
						messages = append(messages, multiple[i])
					}
					w.WriteHeader(http.StatusCreated)
					return
				}

			} else if r.Method == http.MethodGet {
				sessionKeyEq := query.Get("session_key")
				key := strings.TrimPrefix(sessionKeyEq, "eq.")
				res := make([]SupabaseMessageRow, 0)
				for _, m := range messages {
					if m.SessionKey == key {
						res = append(res, m)
					}
				}
				_ = json.NewEncoder(w).Encode(res)
				return
			} else if r.Method == http.MethodDelete {
				sessionKeyEq := query.Get("session_key")
				key := strings.TrimPrefix(sessionKeyEq, "eq.")
				newMsgs := make([]SupabaseMessageRow, 0)
				for _, m := range messages {
					if m.SessionKey != key {
						newMsgs = append(newMsgs, m)
					}
				}
				messages = newMsgs
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx := context.Background()
	store, err := NewSupabaseStore(server.URL, "test-key")
	if err != nil {
		t.Fatalf("NewSupabaseStore() error = %v", err)
	}

	// 1. Test AddMessage
	if err := store.AddMessage(ctx, "session-1", "user", "Hello Supabase"); err != nil {
		t.Fatalf("AddMessage() error = %v", err)
	}

	// 2. Test AddFullMessage
	fullMsg := providers.Message{
		Role:    "assistant",
		Content: "Hi there!",
	}
	if err := store.AddFullMessage(ctx, "session-1", fullMsg); err != nil {
		t.Fatalf("AddFullMessage() error = %v", err)
	}

	// 3. Test GetHistory
	history, err := store.GetHistory(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetHistory() error = %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(history))
	}
	if history[0].Content != "Hello Supabase" || history[1].Content != "Hi there!" {
		t.Fatalf("unexpected history content: %+v", history)
	}

	// 4. Test Summary
	if err := store.SetSummary(ctx, "session-1", "A friendly conversation"); err != nil {
		t.Fatalf("SetSummary() error = %v", err)
	}
	summary, err := store.GetSummary(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetSummary() error = %v", err)
	}
	if summary != "A friendly conversation" {
		t.Fatalf("unexpected summary: %q", summary)
	}

	// 5. Test ListSessions
	sessList := store.ListSessions()
	if len(sessList) != 1 || sessList[0] != "session-1" {
		t.Fatalf("unexpected sessions list: %+v", sessList)
	}

	// 6. Test TruncateHistory
	if err := store.TruncateHistory(ctx, "session-1", 0); err != nil {
		t.Fatalf("TruncateHistory() error = %v", err)
	}
	history, err = store.GetHistory(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetHistory() after truncate error = %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("expected 0 messages after truncate, got %d", len(history))
	}
}
