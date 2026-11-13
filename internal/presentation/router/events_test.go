package router

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/application/events"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/domain"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/infrastructure/security"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/handlers"
	"github.com/JoseGaldamez/api_tickethn_grupo3_final_project/internal/presentation/middleware"
	"github.com/go-chi/chi/v5"
)

const testEventID = "80000000-0000-4000-8000-000000000001"

type eventIdentity struct{}

func (eventIdentity) Authenticate(_ context.Context, token string) (domain.User, error) {
	if token == "buyer" {
		return domain.User{ID: token, Roles: []domain.Role{domain.Buyer}}, nil
	}
	return domain.User{ID: token, Roles: []domain.Role{domain.Organizer}}, nil
}

type eventRepoStub struct {
	events.Repository
	event           domain.Event
	createErr       error
	updateErr       error
	confirmationErr error
	committed       bool
	createCalls     int
}

func (s *eventRepoStub) Create(_ context.Context, event domain.Event) (domain.Event, error) {
	s.createCalls++
	if s.createErr != nil && !s.committed {
		return domain.Event{}, s.createErr
	}
	s.event = event
	return event, s.createErr
}

func (s *eventRepoStub) OwnedEvent(_ context.Context, owner, _ string) (domain.Event, error) {
	if s.event.OrganizerID != owner {
		return domain.Event{}, domain.ErrEventNotFound
	}
	return s.event, nil
}

func (s *eventRepoStub) WithEvent(_ context.Context, _ string, action func(events.EventTransaction, domain.Event) error) error {
	return action(eventTxStub{repo: s}, s.event)
}

func (s *eventRepoStub) ConfirmCover(_ context.Context, id, url string) (bool, error) {
	return s.event.ID == id && s.event.CoverURL != nil && *s.event.CoverURL == url, s.confirmationErr
}

func (s *eventRepoStub) CoverReferenced(_ context.Context, url string) (bool, error) {
	return s.event.CoverURL != nil && *s.event.CoverURL == url, nil
}

func (s *eventRepoStub) Ticket(_ context.Context, buyer, id string) (domain.Ticket, error) {
	if buyer != "buyer" {
		return domain.Ticket{}, domain.ErrEventNotFound
	}
	return domain.Ticket{ID: id, EventID: testEventID, Code: "TKHN-123"}, nil
}

type eventTxStub struct {
	events.EventTransaction
	repo *eventRepoStub
}

func (s eventTxStub) SetCover(url string) (domain.Event, error) {
	if s.repo.updateErr != nil {
		return domain.Event{}, s.repo.updateErr
	}
	s.repo.event.CoverURL = &url
	return s.repo.event, nil
}

type coverStub struct {
	fail    bool
	keys    []string
	deleted []string
}

func (s *coverStub) Upload(_ context.Context, key string, cover events.Cover) (string, error) {
	s.keys = append(s.keys, key)
	if s.fail || cover.MediaType != "image/png" {
		return "", errors.New("storage failed")
	}
	return "https://example.test/" + key, nil
}

func (s *coverStub) Delete(_ context.Context, url string) error {
	s.deleted = append(s.deleted, url)
	return nil
}

func eventTestRouter(repo events.Repository, store events.CoverStore) chi.Router {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := events.New(repo, store, security.NewTicketQR([]byte("dedicated-test-key-for-qr-signing")), logger)
	r := chi.NewRouter()
	Events(r, handlers.NewEvents(service, logger), middleware.RequireJWT(eventIdentity{}, logger))
	return r
}

func pngCover(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func formRequest(t *testing.T, method, path, token string, title bool, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if title {
		if err := writer.WriteField("title", " Concierto "); err != nil {
			t.Fatal(err)
		}
	}
	if data != nil {
		// The filename and part's default octet-stream type are intentionally misleading.
		file, err := writer.CreateFormFile("cover", "not-an-image.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, &body)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	return r
}

func TestCreateEvent(t *testing.T) {
	valid := pngCover(t)
	for _, tc := range []struct {
		name, token                                   string
		data                                          []byte
		uploadFails, dbFails, committed, confirmFails bool
		status, creates, uploads, deletes             int
	}{
		{name: "draft without cover", token: "owner", status: 201, creates: 1},
		{name: "valid cover with wrong MIME", token: "owner", data: valid, status: 201, creates: 1, uploads: 1},
		{name: "unsupported image", token: "owner", data: []byte("GIF89a"), status: 415},
		{name: "large image", token: "owner", data: make([]byte, 5*1024*1024+1), status: 413},
		{name: "buyer cannot create", token: "buyer", data: valid, status: 403},
		{name: "upload error", token: "owner", data: valid, uploadFails: true, status: 503, uploads: 1},
		{name: "failed insert cleans cover", token: "owner", data: valid, dbFails: true, status: 500, creates: 1, uploads: 1, deletes: 1},
		{name: "committed insert after connection error", token: "owner", data: valid, dbFails: true, committed: true, status: 201, creates: 1, uploads: 1},
		{name: "unverified insert retains cover", token: "owner", data: valid, dbFails: true, confirmFails: true, status: 500, creates: 1, uploads: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, storage := &eventRepoStub{committed: tc.committed}, &coverStub{fail: tc.uploadFails}
			if tc.dbFails {
				repo.createErr = errors.New("insert failed")
			}
			if tc.confirmFails {
				repo.confirmationErr = errors.New("confirmation failed")
			}
			w := httptest.NewRecorder()
			eventTestRouter(repo, storage).ServeHTTP(w, formRequest(t, http.MethodPost, "/api/organizer/events", tc.token, true, tc.data))
			if w.Code != tc.status || repo.createCalls != tc.creates || len(storage.keys) != tc.uploads || len(storage.deleted) != tc.deletes {
				t.Fatalf("status=%d body=%s creates=%d uploads=%v deletes=%v", w.Code, w.Body, repo.createCalls, storage.keys, storage.deleted)
			}
			if w.Code == 201 && (repo.event.OrganizerID != tc.token || repo.event.Title != "Concierto" ||
				(repo.event.CoverURL == nil) != (tc.data == nil)) {
				t.Fatalf("unexpected event: %+v", repo.event)
			}
			if tc.uploads == 1 && (!strings.HasSuffix(storage.keys[0], ".png") || strings.Contains(storage.keys[0], "not-an-image")) {
				t.Fatalf("unexpected key: %s", storage.keys[0])
			}
		})
	}
}

func TestReplaceEventCover(t *testing.T) {
	valid := pngCover(t)
	oldURL := "https://example.test/old.png"
	for _, tc := range []struct {
		name, token              string
		dbFails, uploadFails     bool
		status, uploads, deletes int
	}{
		{name: "success", token: "owner", status: 200, uploads: 1, deletes: 1},
		{name: "foreign event", token: "another", status: 404},
		{name: "database failure", token: "owner", dbFails: true, status: 500, uploads: 1, deletes: 1},
		{name: "storage failure", token: "owner", uploadFails: true, status: 503, uploads: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &eventRepoStub{event: domain.Event{ID: testEventID, OrganizerID: "owner", CoverURL: &oldURL}}
			if tc.dbFails {
				repo.updateErr = errors.New("update failed")
			}
			store := &coverStub{fail: tc.uploadFails}
			w := httptest.NewRecorder()
			eventTestRouter(repo, store).ServeHTTP(w, formRequest(t, http.MethodPut, "/api/organizer/events/"+testEventID+"/cover", tc.token, false, valid))
			if w.Code != tc.status || len(store.keys) != tc.uploads || len(store.deleted) != tc.deletes {
				t.Fatalf("status=%d body=%s uploads=%v deletes=%v", w.Code, w.Body, store.keys, store.deleted)
			}
			if w.Code != 200 && *repo.event.CoverURL != oldURL {
				t.Fatal("the original cover was lost")
			}
			if tc.deletes == 1 && (store.deleted[0] == oldURL) != (w.Code == 200) {
				t.Fatalf("deleted wrong object: %s", store.deleted[0])
			}
		})
	}
}

func TestEventOwnershipAndQR(t *testing.T) {
	r := eventTestRouter(&eventRepoStub{event: domain.Event{ID: testEventID, OrganizerID: "owner", Status: domain.Draft}}, nil)
	for _, tc := range []struct {
		name, method, path, token, body string
		status                          int
	}{
		{"foreign edit", "PATCH", "/api/organizer/events/" + testEventID, "another", `{"title":"Other title"}`, 404},
		{"incomplete publication", "POST", "/api/organizer/events/" + testEventID + "/publish", "owner", "", 409},
		{"own ticket", "GET", "/api/tickets/" + testEventID, "buyer", "", 200},
		{"wrong role", "GET", "/api/tickets/" + testEventID, "another", "", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+tc.token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if tc.name == "own ticket" && !strings.Contains(w.Body.String(), `"qr_payload":"tickethn-ticket-v1.`+testEventID+"."+testEventID+".") {
				t.Fatalf("missing signed payload: %s", w.Body)
			}
		})
	}
}
