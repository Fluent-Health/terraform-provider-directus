package directus

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// newTestClient returns a client without the retry transport.
func newTestClient(srv *httptest.Server) *Client {
	return NewClient(srv.URL, "tok", WithHTTPClient(srv.Client()))
}

func writeData(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"data": v}); err != nil {
		t.Fatal(err)
	}
}

func TestDo_SendsBearerAndParsesErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Invalid payload. \"name\" is required.","extensions":{"code":"INVALID_PAYLOAD"}}]}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).CreateFolder(context.Background(), &Folder{})
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if ae.StatusCode != 400 || ae.Code != "INVALID_PAYLOAD" || ae.Message != `Invalid payload. "name" is required.` {
		t.Fatalf("APIError = %+v", ae)
	}
}

func TestGetFolder_NotFoundFromListing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/folders" || r.URL.Query().Get("limit") != "-1" {
			t.Errorf("unexpected request %s", r.URL)
		}
		writeData(t, w, []Folder{{ID: "a", Name: "A"}})
	}))
	defer srv.Close()

	c := newTestClient(srv)
	f, err := c.GetFolder(context.Background(), "a")
	if err != nil || f.Name != "A" {
		t.Fatalf("GetFolder(a) = %+v, %v", f, err)
	}
	if _, err := c.GetFolder(context.Background(), "b"); !IsNotFound(err) {
		t.Fatalf("GetFolder(b) err = %v, want ErrNotFound", err)
	}
}

// A 403 on the listing is a permission problem, never "not found": treating it
// as gone would plan every object for re-creation.
func TestGetFolder_ForbiddenIsNotNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"message":"You don't have permission to access this.","extensions":{"code":"FORBIDDEN"}}]}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetFolder(context.Background(), "a")
	if IsNotFound(err) {
		t.Fatal("403 was reported as not found")
	}
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != "FORBIDDEN" {
		t.Fatalf("err = %v, want FORBIDDEN APIError", err)
	}
}

func TestListCache_OneRequestForConcurrentReaders(t *testing.T) {
	var calls int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		<-release
		writeData(t, w, []Folder{{ID: "a"}, {ID: "b"}})
	}))
	defer srv.Close()

	c := newTestClient(srv)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := c.GetFolder(context.Background(), id)
			errs <- err
		}([]string{"a", "b"}[i%2])
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("GET /folders calls = %d, want 1", got)
	}
}

func TestListCache_FailureIsNotCached(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeData(t, w, []Folder{{ID: "a"}})
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if _, err := c.GetFolder(context.Background(), "a"); err == nil {
		t.Fatal("first read: want error")
	}
	if _, err := c.GetFolder(context.Background(), "a"); err != nil {
		t.Fatalf("second read: %v", err)
	}
}

func TestListCache_WriteInvalidates(t *testing.T) {
	var lists int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if atomic.AddInt32(&lists, 1) == 1 {
				writeData(t, w, []Folder{})
				return
			}
			writeData(t, w, []Folder{{ID: "new", Name: "N"}})
		case http.MethodPost:
			writeData(t, w, Folder{ID: "new", Name: "N"})
		}
	}))
	defer srv.Close()

	c := newTestClient(srv)
	if _, err := c.GetFolder(context.Background(), "new"); !IsNotFound(err) {
		t.Fatalf("before create: err = %v", err)
	}
	if _, err := c.CreateFolder(context.Background(), &Folder{ID: "new", Name: "N"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetFolder(context.Background(), "new"); err != nil {
		t.Fatalf("after create: %v (stale cache)", err)
	}
}

// UpdateFolder must send parent:null to move a folder to the root; omitting
// the key would leave it under its old parent.
func TestUpdateFolder_SendsNullParent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if v, ok := body["parent"]; !ok || v != nil {
			t.Errorf("body parent = %v (present=%v), want explicit null", v, ok)
		}
		if r.URL.Path != "/folders/x" || r.Method != http.MethodPatch {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		writeData(t, w, Folder{ID: "x", Name: "X"})
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).UpdateFolder(context.Background(), &Folder{ID: "x", Name: "X"}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteFolder_AnnotatesForeignKeyViolation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		// Verbatim from Directus 12.4.1 deleting a folder that has a subfolder.
		_, _ = w.Write([]byte(`{"errors":[{"message":"delete from \"directus_folders\" where \"id\" in ($1) - update or delete on table \"directus_folders\" violates foreign key constraint \"directus_folders_parent_foreign\" on table \"directus_folders\"","extensions":{"code":"INTERNAL_SERVER_ERROR"}}]}`))
	}))
	defer srv.Close()

	err := newTestClient(srv).DeleteFolder(context.Background(), "p")
	var ae *APIError
	if !errors.As(err, &ae) || ae.StatusCode != 500 {
		t.Fatalf("err = %v, want wrapped 500 APIError", err)
	}
	if !strings.Contains(err.Error(), "still referenced") {
		t.Fatalf("err = %v, want the referenced hint", err)
	}
}
