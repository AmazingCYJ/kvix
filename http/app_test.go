package main

import (
	"encoding/json"
	"io"
	kvix "kvix"
	common "kvix/common"
	redisstore "kvix/redis"
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

func TestRedisStringSetGetDelTTL(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/redis/string/set", `{"key":"name","value":"alice"}`)
	assertStatus(t, resp, http.StatusOK)
	body := assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["key"] != "name" || data["value"] != "alice" {
		t.Fatalf("expected set response to echo key/value, got %#v", data)
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/string/get", `{"key":"name"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["key"] != "name" || data["value"] != "alice" {
		t.Fatalf("expected get response to echo key/value, got %#v", data)
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/string/ttl", `{"key":"name"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["ttl_seconds"] != float64(-1) {
		t.Fatalf("expected ttl_seconds to be -1 for persistent key, got %#v", data["ttl_seconds"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/string/expire", `{"key":"name","ttl_seconds":3}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["updated"] != true {
		t.Fatalf("expected updated=true, got %#v", data["updated"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/string/ttl", `{"key":"name"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	ttlSeconds, ok := data["ttl_seconds"].(float64)
	if !ok {
		t.Fatalf("expected ttl_seconds number, got %#v", data["ttl_seconds"])
	}
	if ttlSeconds <= 0 || ttlSeconds > 3 {
		t.Fatalf("expected ttl_seconds in (0,3], got %#v", ttlSeconds)
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/string/del", `{"key":"name"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["deleted"] != true {
		t.Fatalf("expected deleted=true, got %#v", data["deleted"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/string/get", `{"key":"name"}`)
	assertStatus(t, resp, http.StatusNotFound)
	_ = assertJSONMessage(t, resp, http.StatusNotFound, "key not found")
}

func TestRedisStringValidationAndWrongType(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/redis/string/set", `{"value":"alice"}`)
	assertStatus(t, resp, http.StatusBadRequest)
	_ = assertJSONMessage(t, resp, http.StatusBadRequest, "missing key")

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/string/set", `{"key":"name"}`)
	assertStatus(t, resp, http.StatusBadRequest)
	_ = assertJSONMessage(t, resp, http.StatusBadRequest, "missing value")

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/string/expire", `{"key":"name"}`)
	assertStatus(t, resp, http.StatusBadRequest)
	_ = assertJSONMessage(t, resp, http.StatusBadRequest, "missing ttl_seconds")

	store := redisstore.NewRedisDataStoreFromDB(db)
	added, err := store.HSet([]byte("profile"), []byte("name"), []byte("alice"))
	if err != nil {
		t.Fatalf("failed to seed hash key: %v", err)
	}
	if !added {
		t.Fatalf("expected hash field to be added")
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/string/get", `{"key":"profile"}`)
	assertStatus(t, resp, http.StatusBadRequest)
	_ = assertJSONMessage(t, resp, http.StatusBadRequest, "wrong type")
}

func TestRedisHashCommands(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/redis/hash/hset", `{"key":"profile","field":"name","value":"alice"}`)
	assertStatus(t, resp, http.StatusOK)
	body := assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["added"] != true {
		t.Fatalf("expected added=true on first hset, got %#v", data["added"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/hash/hset", `{"key":"profile","field":"name","value":"bob"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["added"] != false {
		t.Fatalf("expected added=false on overwrite, got %#v", data["added"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/hash/hget", `{"key":"profile","field":"name"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["key"] != "profile" || data["field"] != "name" || data["value"] != "bob" {
		t.Fatalf("expected hget response to include key/field/value, got %#v", data)
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/hash/hexists", `{"key":"profile","field":"name"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["exists"] != true {
		t.Fatalf("expected exists=true, got %#v", data["exists"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/hash/hlen", `{"key":"profile"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["count"] != float64(1) {
		t.Fatalf("expected count=1, got %#v", data["count"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/hash/hdel", `{"key":"profile","field":"name"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["deleted"] != true {
		t.Fatalf("expected deleted=true, got %#v", data["deleted"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/hash/hget", `{"key":"profile","field":"name"}`)
	assertStatus(t, resp, http.StatusNotFound)
	_ = assertJSONMessage(t, resp, http.StatusNotFound, "key not found")
}

func TestRedisListCommands(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/redis/list/lpush", `{"key":"numbers","values":["a","b"]}`)
	assertStatus(t, resp, http.StatusOK)
	body := assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["count"] != float64(2) {
		t.Fatalf("expected count=2 after lpush, got %#v", data["count"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/list/rpush", `{"key":"numbers","values":["c","d"]}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["count"] != float64(4) {
		t.Fatalf("expected count=4 after rpush, got %#v", data["count"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/list/llen", `{"key":"numbers"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["count"] != float64(4) {
		t.Fatalf("expected llen count=4, got %#v", data["count"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/list/lrange", `{"key":"numbers","start":0,"stop":-1}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	values, ok := data["values"].([]interface{})
	if !ok {
		t.Fatalf("expected values array, got %#v", data["values"])
	}
	expectedBeforePop := []string{"b", "a", "c", "d"}
	if len(values) != len(expectedBeforePop) {
		t.Fatalf("expected %d values before pop, got %#v", len(expectedBeforePop), values)
	}
	for index, expected := range expectedBeforePop {
		if values[index] != expected {
			t.Fatalf("expected values[%d]=%q, got %#v", index, expected, values[index])
		}
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/list/lpop", `{"key":"numbers"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["value"] != "b" {
		t.Fatalf("expected lpop value %q, got %#v", "b", data["value"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/list/rpop", `{"key":"numbers"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["value"] != "d" {
		t.Fatalf("expected rpop value %q, got %#v", "d", data["value"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/list/lrange", `{"key":"numbers","start":0,"stop":-1}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	values, ok = data["values"].([]interface{})
	if !ok {
		t.Fatalf("expected values array, got %#v", data["values"])
	}
	expectedAfterPop := []string{"a", "c"}
	if len(values) != len(expectedAfterPop) {
		t.Fatalf("expected %d values after pop, got %#v", len(expectedAfterPop), values)
	}
	for index, expected := range expectedAfterPop {
		if values[index] != expected {
			t.Fatalf("expected values[%d]=%q, got %#v", index, expected, values[index])
		}
	}
}

func TestRedisWrongTypeReturns400(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/redis/string/set", `{"key":"plain","value":"alice"}`)
	assertStatus(t, resp, http.StatusOK)

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/hash/hget", `{"key":"plain","field":"name"}`)
	assertStatus(t, resp, http.StatusBadRequest)
	_ = assertJSONMessage(t, resp, http.StatusBadRequest, "wrong type")

	store := redisstore.NewRedisDataStoreFromDB(db)
	added, err := store.HSet([]byte("profile"), []byte("name"), []byte("alice"))
	if err != nil {
		t.Fatalf("failed to seed hash key: %v", err)
	}
	if !added {
		t.Fatalf("expected hash field to be added")
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/list/llen", `{"key":"profile"}`)
	assertStatus(t, resp, http.StatusBadRequest)
	_ = assertJSONMessage(t, resp, http.StatusBadRequest, "wrong type")
}

func TestRedisSetCommands(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/redis/set/sadd", `{"key":"users","members":["alice","bob","alice"]}`)
	assertStatus(t, resp, http.StatusOK)
	body := assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["count"] != float64(2) {
		t.Fatalf("expected sadd count=2, got %#v", data["count"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/set/sismember", `{"key":"users","member":"alice"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["exists"] != true {
		t.Fatalf("expected exists=true for alice, got %#v", data["exists"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/set/scard", `{"key":"users"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["count"] != float64(2) {
		t.Fatalf("expected scard count=2, got %#v", data["count"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/set/smembers", `{"key":"users"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	members, ok := data["members"].([]interface{})
	if !ok {
		t.Fatalf("expected members array, got %#v", data["members"])
	}
	assertStringSetEquals(t, members, []string{"alice", "bob"})

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/set/srem", `{"key":"users","members":["alice","carol"]}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["count"] != float64(1) {
		t.Fatalf("expected srem count=1, got %#v", data["count"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/set/sismember", `{"key":"users","member":"alice"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["exists"] != false {
		t.Fatalf("expected exists=false for removed member, got %#v", data["exists"])
	}
}

func TestRedisZSetCommands(t *testing.T) {
	app, db := setupTestApp(t)
	defer func() { _ = db.Close() }()

	resp := performRequest(t, app, http.MethodPost, "/api/v1/redis/zset/zadd", `{"key":"ranking","member":"alice","score":10}`)
	assertStatus(t, resp, http.StatusOK)
	body := assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["updated"] != true {
		t.Fatalf("expected updated=true on first zadd, got %#v", data["updated"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/zset/zadd", `{"key":"ranking","member":"bob","score":20}`)
	assertStatus(t, resp, http.StatusOK)

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/zset/zadd", `{"key":"ranking","member":"charlie","score":15}`)
	assertStatus(t, resp, http.StatusOK)

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/zset/zadd", `{"key":"ranking","member":"alice","score":15}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["updated"] != true {
		t.Fatalf("expected updated=true when score changes, got %#v", data["updated"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/zset/zadd", `{"key":"ranking","member":"alice","score":15}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["updated"] != false {
		t.Fatalf("expected updated=false when score stays unchanged, got %#v", data["updated"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/zset/zscore", `{"key":"ranking","member":"alice"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["score"] != float64(15) {
		t.Fatalf("expected zscore=15, got %#v", data["score"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/zset/zcard", `{"key":"ranking"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["count"] != float64(3) {
		t.Fatalf("expected zcard count=3, got %#v", data["count"])
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/zset/zrange", `{"key":"ranking","start":0,"stop":-1}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	members, ok := data["members"].([]interface{})
	if !ok {
		t.Fatalf("expected members array, got %#v", data["members"])
	}
	expectedOrder := []string{"alice", "charlie", "bob"}
	if len(members) != len(expectedOrder) {
		t.Fatalf("expected %d members, got %#v", len(expectedOrder), members)
	}
	for index, expected := range expectedOrder {
		if members[index] != expected {
			t.Fatalf("expected members[%d]=%q, got %#v", index, expected, members[index])
		}
	}

	resp = performRequest(t, app, http.MethodPost, "/api/v1/redis/zset/zrem", `{"key":"ranking","member":"bob"}`)
	assertStatus(t, resp, http.StatusOK)
	body = assertJSONMessage(t, resp, http.StatusOK, "ok")
	data, ok = body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", body["data"])
	}
	if data["updated"] != true {
		t.Fatalf("expected updated=true on zrem hit, got %#v", data["updated"])
	}
}

func setupTestApp(t *testing.T) (*fiber.App, *kvix.DB) {
	t.Helper()
	option := common.DefaultOptions
	option.DirPath = t.TempDir()

	db, err := kvix.Open(option)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	redisStore := redisstore.NewRedisDataStoreFromDB(db)
	return newApp(db, redisStore), db
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

func assertStringSetEquals(t *testing.T, actual []interface{}, expected []string) {
	t.Helper()

	got := make(map[string]struct{}, len(actual))
	for _, item := range actual {
		value, ok := item.(string)
		if !ok {
			t.Fatalf("expected string item, got %#v", item)
		}
		got[value] = struct{}{}
	}

	want := make(map[string]struct{}, len(expected))
	for _, item := range expected {
		want[item] = struct{}{}
	}

	if len(got) != len(want) {
		t.Fatalf("expected set size %d, got %#v", len(want), got)
	}
	for item := range want {
		if _, ok := got[item]; !ok {
			t.Fatalf("expected item %q in set, got %#v", item, got)
		}
	}
}
