package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
)

const (
	defaultSessionsTable = "picoclaw_sessions"
	defaultMessagesTable = "picoclaw_messages"
	defaultHTTPTimeout   = 20 * time.Second
)

// SupabaseStore implements the Store interface backed by Supabase PostgreSQL
// via the PostgREST HTTPS API.
// It persists sessions, conversation history, summaries, and scopes durably
// across restarts, redeployments, and multiple Render instances.
type SupabaseStore struct {
	restBaseURL   string
	apiKey        string
	client        *http.Client
	sessionsTable string
	messagesTable string
	locks         [numLockShards]sync.Mutex
}

// SupabaseSessionRow represents a row in the picoclaw_sessions table.
type SupabaseSessionRow struct {
	SessionKey string          `json:"session_key"`
	Summary    string          `json:"summary"`
	Scope      json.RawMessage `json:"scope,omitempty"`
	Aliases    []string        `json:"aliases,omitempty"`
	CreatedAt  *time.Time      `json:"created_at,omitempty"`
	UpdatedAt  *time.Time      `json:"updated_at,omitempty"`
}

// SupabaseMessageRow represents a row in the picoclaw_messages table.
type SupabaseMessageRow struct {
	ID         int64           `json:"id,omitempty"`
	SessionKey string          `json:"session_key"`
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	RawMessage json.RawMessage `json:"raw_message,omitempty"`
	CreatedAt  *time.Time      `json:"created_at,omitempty"`
}

// NewSupabaseStore creates a new persistent store backed by Supabase.
// supabaseURL should be the base project URL, e.g. "https://xxxx.supabase.co".
// supabaseKey can be the service_role key or anon key.
func NewSupabaseStore(supabaseURL, supabaseKey string) (*SupabaseStore, error) {
	supabaseURL = strings.TrimSpace(supabaseURL)
	supabaseKey = strings.TrimSpace(supabaseKey)

	if supabaseURL == "" {
		return nil, fmt.Errorf("memory: supabase url is required")
	}
	if supabaseKey == "" {
		return nil, fmt.Errorf("memory: supabase key is required")
	}

	restBase := strings.TrimRight(supabaseURL, "/")
	if !strings.HasSuffix(restBase, "/rest/v1") {
		restBase += "/rest/v1"
	}

	sessionsTable := os.Getenv("PICOCLAW_SUPABASE_TABLE_SESSIONS")
	if sessionsTable == "" {
		sessionsTable = defaultSessionsTable
	}

	messagesTable := os.Getenv("PICOCLAW_SUPABASE_TABLE_MESSAGES")
	if messagesTable == "" {
		messagesTable = defaultMessagesTable
	}

	store := &SupabaseStore{
		restBaseURL:   restBase,
		apiKey:        supabaseKey,
		client:        &http.Client{Timeout: defaultHTTPTimeout},
		sessionsTable: sessionsTable,
		messagesTable: messagesTable,
	}

	return store, nil
}

func (s *SupabaseStore) lockShard(key string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &s.locks[h.Sum32()%numLockShards]
}

func (s *SupabaseStore) newRequest(ctx context.Context, method, endpoint string, body any) (*http.Request, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("memory: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	reqURL := s.restBaseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("memory: create request: %w", err)
	}

	req.Header.Set("apikey", s.apiKey)
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (s *SupabaseStore) doRequest(req *http.Request) ([]byte, int, error) {
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("memory: supabase http request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("memory: read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return respBody, resp.StatusCode, fmt.Errorf("memory: supabase error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, resp.StatusCode, nil
}

// ensureSessionExists creates or touches the session row in picoclaw_sessions.
func (s *SupabaseStore) ensureSessionExists(ctx context.Context, sessionKey string) error {
	now := time.Now().UTC()
	row := SupabaseSessionRow{
		SessionKey: sessionKey,
		UpdatedAt:  &now,
	}

	req, err := s.newRequest(ctx, http.MethodPost, "/"+s.sessionsTable+"?on_conflict=session_key", row)
	if err != nil {
		return err
	}
	req.Header.Set("Prefer", "resolution=merge-duplicates,return=minimal")

	_, _, err = s.doRequest(req)
	if err != nil {
		return fmt.Errorf("memory: upsert session %q: %w", sessionKey, err)
	}
	return nil
}

// AddMessage appends a simple text message to a session in Supabase.
func (s *SupabaseStore) AddMessage(ctx context.Context, sessionKey, role, content string) error {
	mu := s.lockShard(sessionKey)
	mu.Lock()
	defer mu.Unlock()

	if err := s.ensureSessionExists(ctx, sessionKey); err != nil {
		return err
	}

	now := time.Now().UTC()
	rawMsg, _ := json.Marshal(map[string]string{"role": role, "content": content})

	row := SupabaseMessageRow{
		SessionKey: sessionKey,
		Role:       role,
		Content:    content,
		RawMessage: rawMsg,
		CreatedAt:  &now,
	}

	req, err := s.newRequest(ctx, http.MethodPost, "/"+s.messagesTable, row)
	if err != nil {
		return err
	}
	req.Header.Set("Prefer", "return=minimal")

	_, _, err = s.doRequest(req)
	if err != nil {
		return fmt.Errorf("memory: insert message for %q: %w", sessionKey, err)
	}
	return nil
}

// AddFullMessage appends a complete message (with tool calls, etc.) to a session in Supabase.
func (s *SupabaseStore) AddFullMessage(ctx context.Context, sessionKey string, msg providers.Message) error {
	mu := s.lockShard(sessionKey)
	mu.Lock()
	defer mu.Unlock()

	if err := s.ensureSessionExists(ctx, sessionKey); err != nil {
		return err
	}

	rawMsg, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("memory: marshal full message: %w", err)
	}

	now := time.Now().UTC()
	row := SupabaseMessageRow{
		SessionKey: sessionKey,
		Role:       msg.Role,
		Content:    msg.Content,
		RawMessage: rawMsg,
		CreatedAt:  &now,
	}

	req, err := s.newRequest(ctx, http.MethodPost, "/"+s.messagesTable, row)
	if err != nil {
		return err
	}
	req.Header.Set("Prefer", "return=minimal")

	_, _, err = s.doRequest(req)
	if err != nil {
		return fmt.Errorf("memory: insert full message for %q: %w", sessionKey, err)
	}
	return nil
}

// GetHistory returns all messages for a session in chronological insertion order.
func (s *SupabaseStore) GetHistory(ctx context.Context, sessionKey string) ([]providers.Message, error) {
	mu := s.lockShard(sessionKey)
	mu.Lock()
	defer mu.Unlock()

	endpoint := fmt.Sprintf("/%s?session_key=eq.%s&order=id.asc",
		s.messagesTable, url.QueryEscape(sessionKey))

	req, err := s.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return []providers.Message{}, err
	}

	data, _, err := s.doRequest(req)
	if err != nil {
		return []providers.Message{}, err
	}

	var rows []SupabaseMessageRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return []providers.Message{}, fmt.Errorf("memory: decode messages: %w", err)
	}

	result := make([]providers.Message, 0, len(rows))
	for _, r := range rows {
		if len(r.RawMessage) > 0 && string(r.RawMessage) != "null" {
			var msg providers.Message
			if err := json.Unmarshal(r.RawMessage, &msg); err == nil {
				result = append(result, msg)
				continue
			}
		}
		result = append(result, providers.Message{
			Role:    r.Role,
			Content: r.Content,
		})
	}

	return result, nil
}

// GetSummary returns the conversation summary for a session from Supabase.
func (s *SupabaseStore) GetSummary(ctx context.Context, sessionKey string) (string, error) {
	mu := s.lockShard(sessionKey)
	mu.Lock()
	defer mu.Unlock()

	endpoint := fmt.Sprintf("/%s?session_key=eq.%s&select=summary",
		s.sessionsTable, url.QueryEscape(sessionKey))

	req, err := s.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	data, _, err := s.doRequest(req)
	if err != nil {
		return "", err
	}

	var rows []struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) == 0 {
		return "", nil
	}

	return rows[0].Summary, nil
}

// SetSummary updates the conversation summary for a session in Supabase.
func (s *SupabaseStore) SetSummary(ctx context.Context, sessionKey, summary string) error {
	mu := s.lockShard(sessionKey)
	mu.Lock()
	defer mu.Unlock()

	now := time.Now().UTC()
	row := SupabaseSessionRow{
		SessionKey: sessionKey,
		Summary:    summary,
		UpdatedAt:  &now,
	}

	req, err := s.newRequest(ctx, http.MethodPost, "/"+s.sessionsTable+"?on_conflict=session_key", row)
	if err != nil {
		return err
	}
	req.Header.Set("Prefer", "resolution=merge-duplicates,return=minimal")

	_, _, err = s.doRequest(req)
	if err != nil {
		return fmt.Errorf("memory: set summary for %q: %w", sessionKey, err)
	}
	return nil
}

// TruncateHistory removes older messages, keeping only the last keepLast messages.
func (s *SupabaseStore) TruncateHistory(ctx context.Context, sessionKey string, keepLast int) error {
	mu := s.lockShard(sessionKey)
	mu.Lock()
	defer mu.Unlock()

	escapedKey := url.QueryEscape(sessionKey)

	if keepLast <= 0 {
		endpoint := fmt.Sprintf("/%s?session_key=eq.%s", s.messagesTable, escapedKey)
		req, err := s.newRequest(ctx, http.MethodDelete, endpoint, nil)
		if err != nil {
			return err
		}
		_, _, err = s.doRequest(req)
		return err
	}

	// Fetch the message IDs for the session, ordered newest first with limit = keepLast
	endpoint := fmt.Sprintf("/%s?session_key=eq.%s&select=id&order=id.desc&limit=%d",
		s.messagesTable, escapedKey, keepLast)

	req, err := s.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}

	data, _, err := s.doRequest(req)
	if err != nil {
		return err
	}

	var rows []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return err
	}

	if len(rows) < keepLast {
		// Nothing to truncate
		return nil
	}

	// The oldest among the kept messages is rows[len(rows)-1]
	cutoffID := rows[len(rows)-1].ID

	// Delete messages with id < cutoffID
	delEndpoint := fmt.Sprintf("/%s?session_key=eq.%s&id=lt.%d",
		s.messagesTable, escapedKey, cutoffID)

	delReq, err := s.newRequest(ctx, http.MethodDelete, delEndpoint, nil)
	if err != nil {
		return err
	}

	_, _, err = s.doRequest(delReq)
	return err
}

// SetHistory replaces all messages in a session with the provided history.
func (s *SupabaseStore) SetHistory(ctx context.Context, sessionKey string, history []providers.Message) error {
	mu := s.lockShard(sessionKey)
	mu.Lock()
	defer mu.Unlock()

	if err := s.ensureSessionExists(ctx, sessionKey); err != nil {
		return err
	}

	escapedKey := url.QueryEscape(sessionKey)
	delEndpoint := fmt.Sprintf("/%s?session_key=eq.%s", s.messagesTable, escapedKey)
	delReq, err := s.newRequest(ctx, http.MethodDelete, delEndpoint, nil)
	if err != nil {
		return err
	}
	if _, _, err := s.doRequest(delReq); err != nil {
		return fmt.Errorf("memory: clear history: %w", err)
	}

	if len(history) == 0 {
		return nil
	}

	now := time.Now().UTC()
	rows := make([]SupabaseMessageRow, 0, len(history))
	for _, msg := range history {
		rawMsg, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("memory: marshal message: %w", err)
		}
		rows = append(rows, SupabaseMessageRow{
			SessionKey: sessionKey,
			Role:       msg.Role,
			Content:    msg.Content,
			RawMessage: rawMsg,
			CreatedAt:  &now,
		})
	}

	insReq, err := s.newRequest(ctx, http.MethodPost, "/"+s.messagesTable, rows)
	if err != nil {
		return err
	}
	insReq.Header.Set("Prefer", "return=minimal")

	_, _, err = s.doRequest(insReq)
	if err != nil {
		return fmt.Errorf("memory: batch insert history: %w", err)
	}
	return nil
}

// Compact is a no-op for PostgreSQL/Supabase as MVCC cleans up dead rows automatically.
func (s *SupabaseStore) Compact(_ context.Context, _ string) error {
	return nil
}

// ListSessions returns all known session keys in Supabase, ordered by recent activity.
func (s *SupabaseStore) ListSessions() []string {
	endpoint := fmt.Sprintf("/%s?select=session_key&order=updated_at.desc", s.sessionsTable)
	req, err := s.newRequest(context.Background(), http.MethodGet, endpoint, nil)
	if err != nil {
		logger.WarnCF("memory", "supabase list sessions request build failed", map[string]any{"error": err.Error()})
		return []string{}
	}

	data, _, err := s.doRequest(req)
	if err != nil {
		logger.WarnCF("memory", "supabase list sessions failed", map[string]any{"error": err.Error()})
		return []string{}
	}

	var rows []struct {
		SessionKey string `json:"session_key"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return []string{}
	}

	result := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.SessionKey != "" {
			result = append(result, r.SessionKey)
		}
	}
	return result
}

// Close releases any idle connections.
func (s *SupabaseStore) Close() error {
	s.client.CloseIdleConnections()
	return nil
}

// GetSessionMeta returns the current metadata snapshot for sessionKey.
func (s *SupabaseStore) GetSessionMeta(ctx context.Context, sessionKey string) (SessionMeta, error) {
	mu := s.lockShard(sessionKey)
	mu.Lock()
	defer mu.Unlock()

	escapedKey := url.QueryEscape(sessionKey)
	endpoint := fmt.Sprintf("/%s?session_key=eq.%s", s.sessionsTable, escapedKey)

	req, err := s.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return SessionMeta{Key: sessionKey}, err
	}

	data, _, err := s.doRequest(req)
	if err != nil {
		return SessionMeta{Key: sessionKey}, err
	}

	var rows []SupabaseSessionRow
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) == 0 {
		return SessionMeta{Key: sessionKey}, nil
	}

	row := rows[0]
	meta := SessionMeta{
		Key:     row.SessionKey,
		Summary: row.Summary,
		Scope:   row.Scope,
		Aliases: row.Aliases,
	}
	if row.CreatedAt != nil {
		meta.CreatedAt = *row.CreatedAt
	}
	if row.UpdatedAt != nil {
		meta.UpdatedAt = *row.UpdatedAt
	}

	// Count messages
	cntEndpoint := fmt.Sprintf("/%s?session_key=eq.%s&select=id", s.messagesTable, escapedKey)
	cntReq, err := s.newRequest(ctx, http.MethodGet, cntEndpoint, nil)
	if err == nil {
		if cntData, _, err := s.doRequest(cntReq); err == nil {
			var ids []struct {
				ID int64 `json:"id"`
			}
			if json.Unmarshal(cntData, &ids) == nil {
				meta.Count = len(ids)
			}
		}
	}

	return meta, nil
}

// UpsertSessionMeta stores structured session metadata (scope and aliases).
func (s *SupabaseStore) UpsertSessionMeta(ctx context.Context, sessionKey string, scope json.RawMessage, aliases []string) error {
	mu := s.lockShard(sessionKey)
	mu.Lock()
	defer mu.Unlock()

	now := time.Now().UTC()
	row := SupabaseSessionRow{
		SessionKey: sessionKey,
		Scope:      scope,
		Aliases:    aliases,
		UpdatedAt:  &now,
	}

	req, err := s.newRequest(ctx, http.MethodPost, "/"+s.sessionsTable+"?on_conflict=session_key", row)
	if err != nil {
		return err
	}
	req.Header.Set("Prefer", "resolution=merge-duplicates,return=minimal")

	_, _, err = s.doRequest(req)
	return err
}

// ResolveSessionKey maps aliases onto their canonical session key.
func (s *SupabaseStore) ResolveSessionKey(ctx context.Context, sessionKey string) (string, bool, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return "", false, nil
	}

	mu := s.lockShard(sessionKey)
	mu.Lock()
	defer mu.Unlock()

	escapedKey := url.QueryEscape(sessionKey)

	// 1. Direct match on session_key
	endpoint := fmt.Sprintf("/%s?session_key=eq.%s&select=session_key", s.sessionsTable, escapedKey)
	req, err := s.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err == nil {
		if data, _, err := s.doRequest(req); err == nil {
			var rows []struct {
				SessionKey string `json:"session_key"`
			}
			if json.Unmarshal(data, &rows) == nil && len(rows) > 0 {
				return rows[0].SessionKey, true, nil
			}
		}
	}

	// 2. Search in aliases array
	aliasEndpoint := fmt.Sprintf("/%s?aliases=cs.{%s}&select=session_key&limit=1",
		s.sessionsTable, url.QueryEscape(strconv.Quote(sessionKey)))
	req, err = s.newRequest(ctx, http.MethodGet, aliasEndpoint, nil)
	if err == nil {
		if data, _, err := s.doRequest(req); err == nil {
			var rows []struct {
				SessionKey string `json:"session_key"`
			}
			if json.Unmarshal(data, &rows) == nil && len(rows) > 0 {
				return rows[0].SessionKey, true, nil
			}
		}
	}

	return sessionKey, false, nil
}

// PromoteAliasHistory transfers message history from an alias key to the canonical sessionKey.
func (s *SupabaseStore) PromoteAliasHistory(ctx context.Context, sessionKey string, _ json.RawMessage, aliases []string) (bool, error) {
	if len(aliases) == 0 {
		return false, nil
	}

	mu := s.lockShard(sessionKey)
	mu.Lock()
	defer mu.Unlock()

	// Check if canonical session already has messages
	escapedCanonical := url.QueryEscape(sessionKey)
	endpoint := fmt.Sprintf("/%s?session_key=eq.%s&select=id&limit=1", s.messagesTable, escapedCanonical)
	req, err := s.newRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	data, _, err := s.doRequest(req)
	if err != nil {
		return false, err
	}
	var canonicalMsgs []struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(data, &canonicalMsgs) == nil && len(canonicalMsgs) > 0 {
		return false, nil
	}

	// Check aliases for existing messages
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" || alias == sessionKey {
			continue
		}
		escapedAlias := url.QueryEscape(alias)
		checkReq, err := s.newRequest(ctx, http.MethodGet, fmt.Sprintf("/%s?session_key=eq.%s&select=id&limit=1", s.messagesTable, escapedAlias), nil)
		if err != nil {
			continue
		}
		aliasData, _, err := s.doRequest(checkReq)
		if err != nil {
			continue
		}
		var aliasMsgs []struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(aliasData, &aliasMsgs) == nil && len(aliasMsgs) > 0 {
			// Migrate messages from alias to canonical
			patchBody := map[string]string{"session_key": sessionKey}
			patchReq, err := s.newRequest(ctx, http.MethodPatch, fmt.Sprintf("/%s?session_key=eq.%s", s.messagesTable, escapedAlias), patchBody)
			if err != nil {
				return false, err
			}
			if _, _, err := s.doRequest(patchReq); err != nil {
				return false, fmt.Errorf("memory: promote alias history: %w", err)
			}
			return true, nil
		}
	}

	return false, nil
}
