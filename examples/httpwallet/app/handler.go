// Package app adapts HTTP requests and trusted sessions to the verified policy.
package app

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/cruskell/proofsteer-go/examples/httpwallet/policy"
)

type Session struct {
	Account string
	Locked  bool
}

type Handler struct {
	mu       sync.Mutex
	sessions map[string]Session
	balances map[string]int
}

// New copies configuration so callers cannot mutate the session store or race
// with requests. This example's session mapping is trusted, static demo data.
func New(sessions map[string]Session, balances map[string]int) *Handler {
	h := &Handler{sessions: map[string]Session{}, balances: map[string]int{}}
	for token, session := range sessions {
		if token != "" && session.Account != "" {
			h.sessions[token] = session
		}
	}
	for account, balance := range balances {
		h.balances[account] = balance
	}
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != "/v1/withdraw" {
		reject(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		reject(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// Reject ambiguous credentials rather than choosing one header value.
	values := r.Header.Values("Authorization")
	var session Session
	var authenticated bool
	if len(values) == 1 && strings.HasPrefix(values[0], "Bearer ") {
		token := strings.TrimPrefix(values[0], "Bearer ")
		session, authenticated = h.sessions[token]
	}
	if !authenticated {
		w.Header().Set("WWW-Authenticate", "Bearer")
		reject(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	amount, valid := decodeAmount(w, r)
	if !valid {
		reject(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Checking and committing share a lock. Gobra proves the decision, while
	// empirical race/concurrency tests cover this adapter and its state update.
	h.mu.Lock()
	balance, exists := h.balances[session.Account]
	allowed := policy.CanWithdraw(authenticated && exists, session.Locked, amount, balance)
	if !allowed {
		h.mu.Unlock()
		reject(w, http.StatusForbidden, "withdrawal denied")
		return
	}
	remaining := balance - amount
	h.balances[session.Account] = remaining
	h.mu.Unlock()
	_ = json.NewEncoder(w).Encode(struct {
		Balance int `json:"balance"`
	}{remaining})
}

// A single exact key rejects duplicate/case-variant amounts as well as unknown
// fields. The integer decoder rejects fractions and values outside host int.
func decodeAmount(w http.ResponseWriter, r *http.Request) (int, bool) {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return 0, false
	}
	key, err := decoder.Token()
	if err != nil || key != "amount" {
		return 0, false
	}
	var amount *int
	if decoder.Decode(&amount) != nil || amount == nil {
		return 0, false
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return 0, false
	}
	var extra interface{}
	if decoder.Decode(&extra) != io.EOF {
		return 0, false
	}
	return *amount, true
}
func reject(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{message})
}
