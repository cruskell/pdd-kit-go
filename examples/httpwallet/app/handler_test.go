package app_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cruskell/pdd-kit-go/examples/httpwallet/app"
)

func configured() *app.Handler {
	return app.New(map[string]app.Session{"alice": {Account: "a"}, "locked": {Account: "a", Locked: true}, "missing": {Account: "missing"}}, map[string]int{"a": 100})
}
func request(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestWithdrawalHTTP(t *testing.T) {
	cases := []struct {
		name, method, path, token, body string
		want                            int
	}{
		{"valid", "POST", "/v1/withdraw", "alice", `{"amount":40}`, 200},
		{"entire balance", "POST", "/v1/withdraw", "alice", `{"amount":100}`, 200},
		{"zero", "POST", "/v1/withdraw", "alice", `{"amount":0}`, 403},
		{"negative", "POST", "/v1/withdraw", "alice", `{"amount":-1}`, 403},
		{"overdraft", "POST", "/v1/withdraw", "alice", `{"amount":101}`, 403},
		{"locked", "POST", "/v1/withdraw", "locked", `{"amount":1}`, 403},
		{"absent account", "POST", "/v1/withdraw", "missing", `{"amount":1}`, 403},
		{"unknown token", "POST", "/v1/withdraw", "unknown", `{"amount":1}`, 401},
		{"no token", "POST", "/v1/withdraw", "", `{"amount":1}`, 401},
		{"wrong method", "GET", "/v1/withdraw", "alice", `{"amount":1}`, 405},
		{"wrong path", "POST", "/other", "alice", `{"amount":1}`, 404},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := configured()
			w := request(h, tc.method, tc.path, tc.token, tc.body)
			if w.Code != tc.want {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if tc.want == 200 {
				var result struct {
					Balance int `json:"balance"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if (tc.name == "valid" && result.Balance != 60) || (tc.name == "entire balance" && result.Balance != 0) {
					t.Fatalf("wrong balance: %+v", result)
				}
			} else {
				// Denied and malformed requests must leave the full balance spendable.
				if next := request(h, "POST", "/v1/withdraw", "alice", `{"amount":100}`); next.Code != 200 {
					t.Fatalf("rejection changed balance: %s", next.Body.String())
				}
			}
		})
	}
}
func TestMalformedHTTP(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `{"amount":null}`, `{"amount":"1"}`, `{"amount":1.5}`, `{"amount":1,"extra":true}`, `{"amount":1,"amount":100}`, `{"Amount":1}`, `[]`, `{"amount":1} {}`, `{"amount":999999999999999999999999999999}`, strings.Repeat(" ", 1025) + `{"amount":1}`} {
		h := configured()
		if w := request(h, "POST", "/v1/withdraw", "alice", body); w.Code != 400 {
			t.Fatalf("accepted malformed request %q: %d", body, w.Code)
		}
		if w := request(h, "POST", "/v1/withdraw", "alice", `{"amount":100}`); w.Code != 200 {
			t.Fatal("malformed request changed balance")
		}
	}
	h := configured()
	r := httptest.NewRequest("POST", "/v1/withdraw", strings.NewReader(`{"amount":1}`))
	r.Header.Add("Authorization", "Bearer alice")
	r.Header.Add("Authorization", "Bearer locked")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("ambiguous credentials were accepted")
	}
}
func TestConcurrentHTTPWithdrawals(t *testing.T) {
	// Exercise an actual TCP HTTP server and the real handler with more requests
	// than the balance can fund, rather than testing a mock transport.
	server := httptest.NewServer(configured())
	defer server.Close()
	var successes, denied int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := http.NewRequest("POST", server.URL+"/v1/withdraw", strings.NewReader(`{"amount":5}`))
			if err != nil {
				t.Error(err)
				return
			}
			r.Header.Set("Authorization", "Bearer alice")
			response, err := server.Client().Do(r)
			if err != nil {
				t.Error(err)
				return
			}
			defer response.Body.Close()
			_, _ = io.Copy(io.Discard, response.Body)
			switch response.StatusCode {
			case 200:
				atomic.AddInt32(&successes, 1)
			case 403:
				atomic.AddInt32(&denied, 1)
			default:
				t.Errorf("unexpected status %d", response.StatusCode)
			}
		}()
	}
	wg.Wait()
	if successes != 20 || denied != 20 {
		t.Fatalf("fund conservation failed: %d successful, %d denied", successes, denied)
	}
}
