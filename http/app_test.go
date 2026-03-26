package main

import (
	bitcaskmy "bitcask-my"
	common "bitcask-my/common"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestNewRoutesPutGetDelete(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/entries", `{"key":"name","value":"alice"}`)
	assertStatus(t, resp, http.StatusOK)
	body := assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok := body["data"].(map[string]interface{})
	if !ok || data["key"] != "name" || data["value"] != "alice" {
		t.Fatalf("expected fixed DTO in POST response, got %#v", body["data"])
	}

	resp = performRequest(t, app, http.MethodGet, "/api/v1/entries/name", "")
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok || data["key"] != "name" || data["value"] != "alice" {
		t.Fatalf("expected fixed DTO in GET response, got %#v", body["data"])
	}

	resp = performRequest(t, app, http.MethodDelete, "/api/v1/entries/name", "")
	assertStatus(t, resp, http.StatusOK)

	resp = performRequest(t, app, http.MethodGet, "/api/v1/entries/name", "")
	assertStatus(t, resp, http.StatusNotFound)
	_ = assertJSONMessage(t, resp, http.StatusNotFound, "key not found")
}

func TestLegacyRoutesRemoved(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/bitcask/put", `{"key":"legacy","value":"value"}`)
	assertStatus(t, resp, http.StatusNotFound)
	resp = performRequest(t, app, http.MethodGet, "/bitcask/get?key=legacy", "")
	assertStatus(t, resp, http.StatusNotFound)
	resp = performRequest(t, app, http.MethodDelete, "/bitcask/delete?key=legacy", "")
	assertStatus(t, resp, http.StatusNotFound)
	resp = performRequest(t, app, http.MethodGet, "/bitcask/listkeys", "")
	assertStatus(t, resp, http.StatusNotFound)
	resp = performRequest(t, app, http.MethodGet, "/bitcask/stat", "")
	assertStatus(t, resp, http.StatusNotFound)
}

func TestEntryLifecycleUsesFixedDTO(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/entries", `{"key":"name","value":"alice"}`)
	assertStatus(t, resp, http.StatusOK)
	body := assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["key"] != "name" || data["value"] != "alice" {
		t.Fatalf("expected data.key/data.value to match input, got %#v", data)
	}

	resp = performRequest(t, app, http.MethodGet, "/api/v1/entries/name", "")
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["key"] != "name" || data["value"] != "alice" {
		t.Fatalf("expected GET data.key/data.value to match input, got %#v", data)
	}
}

func TestEntryWriteValidation(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	testCases := []struct {
		name           string
		body           string
		expectedStatus int
		expectedMsg    string
	}{
		{
			name:           "missing key",
			body:           `{"value":"alice"}`,
			expectedStatus: http.StatusBadRequest,
			expectedMsg:    "missing key",
		},
		{
			name:           "missing value",
			body:           `{"key":"name"}`,
			expectedStatus: http.StatusBadRequest,
			expectedMsg:    "missing value",
		},
		{
			name:           "malformed json",
			body:           `{`,
			expectedStatus: http.StatusBadRequest,
			expectedMsg:    "failed to decode json",
		},
		{
			name:           "wrong field types",
			body:           `{"key":123,"value":true}`,
			expectedStatus: http.StatusBadRequest,
			expectedMsg:    "failed to decode json",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := performRequest(t, app, http.MethodPost, "/api/v1/entries", tc.body)
			assertStatus(t, resp, tc.expectedStatus)
			_ = assertJSONMessage(t, resp, tc.expectedStatus, tc.expectedMsg)
		})
	}
}

func TestEntryGetMissingKeyReturns404(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodGet, "/api/v1/entries/does-not-exist", "")
	assertStatus(t, resp, http.StatusNotFound)
	_ = assertJSONMessage(t, resp, http.StatusNotFound, "key not found")
}

func TestDeleteIsIdempotent(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodDelete, "/api/v1/entries/not-found", "")
	assertStatus(t, resp, http.StatusOK)
	_ = assertJSONMessage(t, resp, http.StatusOK, "ok")

	resp = performRequest(t, app, http.MethodPost, "/api/v1/entries", `{"key":"name","value":"alice"}`)
	assertStatus(t, resp, http.StatusOK)

	resp = performRequest(t, app, http.MethodDelete, "/api/v1/entries/name", "")
	assertStatus(t, resp, http.StatusOK)
	_ = assertJSONMessage(t, resp, http.StatusOK, "ok")

	resp = performRequest(t, app, http.MethodDelete, "/api/v1/entries/name", "")
	assertStatus(t, resp, http.StatusOK)
	_ = assertJSONMessage(t, resp, http.StatusOK, "ok")
}

func TestHealthKeysAndStatsContracts(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodGet, "/healthz", "")
	assertStatus(t, resp, http.StatusOK)
	body := assertJSONMessage(t, resp, http.StatusOK, "ok")
	healthData, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected health data object, got %#v", body["data"])
	}
	if healthData["status"] != "ok" {
		t.Fatalf("expected health data.status to be %q, got %#v", "ok", healthData["status"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/entries", `{"key":"k1","value":"v1"}`)
	assertStatus(t, resp, http.StatusOK)
	resp = performRequest(t, app, http.MethodPost, "/api/v1/entries", `{"key":"k2","value":"v2"}`)
	assertStatus(t, resp, http.StatusOK)

	resp = performRequest(t, app, http.MethodGet, "/api/v1/keys", "")
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	items, ok := body["data"].([]interface{})
	if !ok {
		t.Fatalf("expected keys array, got %#v", body["data"])
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 keys, got %#v", items)
	}
	got := make(map[string]struct{}, len(items))
	for _, item := range items {
		key, ok := item.(string)
		if !ok {
			t.Fatalf("expected string key, got %#v", item)
		}
		got[key] = struct{}{}
	}
	expected := map[string]struct{}{
		"k1": {},
		"k2": {},
	}
	if len(got) != len(expected) {
		t.Fatalf("expected keys set %#v, got %#v", expected, got)
	}
	for key := range expected {
		if _, ok := got[key]; !ok {
			t.Fatalf("expected key %q in response set, got %#v", key, got)
		}
	}

	resp = performRequest(t, app, http.MethodGet, "/api/v1/stats", "")
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	stats, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected stats object, got %#v", body["data"])
	}
	keyNum, ok := stats["KeyNum"].(float64)
	if !ok {
		t.Fatalf("expected KeyNum to be float64, got %#v", stats["KeyNum"])
	}
	if keyNum != 2 {
		t.Fatalf("expected KeyNum to be 2, got %#v", stats["KeyNum"])
	}
}

func TestBatchWriteEndpoint(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/entries/batch", `{"entries":[{"key":"k1","value":"v1"},{"key":"k2","value":"v2"}]}`)
	assertStatus(t, resp, http.StatusOK)

	body := assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["count"] != float64(2) {
		t.Fatalf("expected count to be 2, got %#v", data["count"])
	}

	resp = performRequest(t, app, http.MethodGet, "/api/v1/entries/k1", "")
	assertStatus(t, resp, http.StatusOK)
	resp = performRequest(t, app, http.MethodGet, "/api/v1/entries/k2", "")
	assertStatus(t, resp, http.StatusOK)
}

func TestBatchRouteTakesPrecedence(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/entries/batch", `{"entries":[]}`)
	assertStatus(t, resp, http.StatusBadRequest)
	_ = assertJSONMessage(t, resp, http.StatusBadRequest, "empty entries")
}

func TestBatchWriteFailureReportsWrittenCount(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/entries/batch", `{"entries":[{"key":"k1","value":"v1"},{"key":"","value":"v2"}]}`)
	assertStatus(t, resp, http.StatusBadRequest)

	body := assertJSONMessage(t, resp, http.StatusBadRequest, "invalid batch entry")
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["written_count"] != float64(1) {
		t.Fatalf("expected written_count to be 1, got %#v", data["written_count"])
	}

	resp = performRequest(t, app, http.MethodGet, "/api/v1/entries/k1", "")
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["key"] != "k1" || data["value"] != "v1" {
		t.Fatalf("expected first entry to remain committed, got %#v", data)
	}
}

func TestBatchWriteInvalidFirstEntryReportsZeroWrittenCount(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/entries/batch", `{"entries":[{"key":"","value":"v1"}]}`)
	assertStatus(t, resp, http.StatusBadRequest)

	body := assertJSONMessage(t, resp, http.StatusBadRequest, "invalid batch entry")
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["written_count"] != float64(0) {
		t.Fatalf("expected written_count to be 0, got %#v", data["written_count"])
	}
}

func TestBatchWriteDuplicateKeysLastWriteWins(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/entries/batch", `{"entries":[{"key":"dup","value":"first"},{"key":"dup","value":"second"}]}`)
	assertStatus(t, resp, http.StatusOK)
	_ = assertJSONMessage(t, resp, http.StatusOK, "ok")

	resp = performRequest(t, app, http.MethodGet, "/api/v1/entries/dup", "")
	assertStatus(t, resp, http.StatusOK)
	body := assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["value"] != "second" {
		t.Fatalf("expected last duplicate write to win, got %#v", data["value"])
	}
}

func TestBatchWriteRejectsWrongFieldTypes(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/entries/batch", `{"entries":[{"key":123,"value":true}]}`)
	assertStatus(t, resp, http.StatusBadRequest)
	_ = assertJSONMessage(t, resp, http.StatusBadRequest, "failed to decode json")
}

func setupTestApp(t *testing.T) (*fiber.App, *bitcaskmy.DB) {
	t.Helper()
	option := common.DefaultOptions
	option.DirPath = t.TempDir()

	db, err := bitcaskmy.Open(option)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	return newApp(db), db
}

func performRequest(t *testing.T, app *fiber.App, method, target, body string) *http.Response {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}

	resp, err := app.Test(request)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func assertStatus(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		t.Fatalf("expected status %d, got %d", expected, resp.StatusCode)
	}
}

func assertJSONMessage(t *testing.T, resp *http.Response, code int, message string) map[string]interface{} {
	t.Helper()
	body := decodeBody(t, resp)
	actualCode, ok := body["code"].(float64)
	if !ok {
		t.Fatalf("expected numeric code, got %#v", body["code"])
	}
	if int(actualCode) != code {
		t.Fatalf("expected code %d, got %#v", code, body["code"])
	}
	if body["message"] != message {
		t.Fatalf("expected message %q, got %#v", message, body["message"])
	}
	return body
}

func decodeBody(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	result := make(map[string]interface{})
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatalf("failed to decode response body: %v; body=%s", err, string(content))
	}
	return result
}
