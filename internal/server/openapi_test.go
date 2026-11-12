package server

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite openapi.json")

// TestOpenAPISpec keeps openapi.json in step with the route table.
func TestOpenAPISpec(t *testing.T) {
	spec, err := Spec((&Server{}).routes())
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile("openapi.json", spec, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if !bytes.Equal(spec, specJSON) {
		t.Fatal("openapi.json is stale: run `go test ./internal/server -run TestOpenAPISpec -update`, then `npm run generate:api` in frontend")
	}
}

func TestOperationIDsAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, rt := range (&Server{}).routes() {
		id := operationID(rt)
		if other, ok := seen[id]; ok {
			t.Errorf("%s %s and %s share operationId %s", rt.method, rt.path, other, id)
		}
		seen[id] = rt.method + " " + rt.path
		if (rt.handle == nil) == (rt.raw == nil) {
			t.Errorf("%s %s: exactly one of handle and raw must be set", rt.method, rt.path)
		}
	}
}

func TestSpecIsServed(t *testing.T) {
	f := newFixture(t)
	resp := f.do("GET", "/api/v1/openapi.json", "", nil)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, specJSON) {
		t.Errorf("spec: %d, %d bytes", resp.StatusCode, len(body))
	}
}

// TestResponsesMatchSpec sends real requests and checks every answer
// against the schema the spec gives for it: required fields present,
// types right, arrays not null.
func TestResponsesMatchSpec(t *testing.T) {
	f := newFixture(t)
	var spec map[string]any
	if err := json.Unmarshal(specJSON, &spec); err != nil {
		t.Fatal(err)
	}

	jobID := ""
	cases := []struct {
		method, path, specPath, body string
	}{
		{"GET", "/health", "/health", ""},
		{"GET", "/session", "/session", ""},
		{"GET", "/info", "/info", ""},
		{"POST", "/jobs", "/jobs", `{"url":"https://example.com/v","quality":"audio"}`},
		{"GET", "/jobs", "/jobs", ""},
		{"GET", "/library", "/library", ""},
		{"GET", "/accounts", "/accounts", ""},
		{"GET", "/settings", "/settings", ""},
		{"PUT", "/settings/max-concurrent-downloads", "/settings/max-concurrent-downloads", `{"value":2}`},
		{"PUT", "/settings/proxy", "/settings/proxy", `{"proxy":"socks5://user:pass@127.0.0.1:1080"}`},
		{"GET", "/tools", "/tools", ""},
		{"GET", "/devices", "/devices", ""},
		{"POST", "/devices", "/devices", `{"name":"script"}`},
		{"POST", "/devices/link", "/devices/link", ""},
		{"GET", "/users", "/users", ""},
		{"POST", "/users/invites", "/users/invites", `{"role":"user"}`},
		{"POST", "/jobs", "/jobs", `{"url":"ftp://bad","quality":"audio"}`},
	}
	for _, c := range cases {
		var body io.Reader
		if c.body != "" {
			body = strings.NewReader(c.body)
		}
		resp := f.do(c.method, "/api/v1"+c.path, f.token, body)
		data, _ := io.ReadAll(resp.Body)
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Errorf("%s %s: not JSON: %s", c.method, c.path, data)
			continue
		}
		if c.method == "POST" && c.path == "/jobs" && resp.StatusCode == http.StatusOK {
			jobID, _ = value.(string)
		}
		if c.body != "" && strings.Contains(c.body, "example.com") && resp.StatusCode != http.StatusOK {
			t.Errorf("%s %s: %d %s", c.method, c.path, resp.StatusCode, data)
		}

		operation := spec["paths"].(map[string]any)[c.specPath].(map[string]any)[strings.ToLower(c.method)].(map[string]any)
		response, ok := operation["responses"].(map[string]any)[fmt.Sprint(resp.StatusCode)].(map[string]any)
		if !ok {
			t.Errorf("%s %s: status %d is not in the spec", c.method, c.path, resp.StatusCode)
			continue
		}
		schema := response["content"].(map[string]any)["application/json"].(map[string]any)["schema"]
		for _, problem := range validate(spec, schema, value, "") {
			t.Errorf("%s %s (%d): %s", c.method, c.path, resp.StatusCode, problem)
		}
	}
	if jobID == "" {
		t.Error("no job was created")
	}
}

// validate is a small JSON Schema check: enough for the subset Spec
// emits ($ref, type, properties, required, items, enum).
func validate(spec map[string]any, schemaValue any, value any, at string) []string {
	s, _ := schemaValue.(map[string]any)
	if ref, ok := s["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		return validate(spec, spec["components"].(map[string]any)["schemas"].(map[string]any)[name], value, at)
	}
	if values, ok := s["enum"].([]any); ok {
		found := false
		for _, v := range values {
			found = found || v == value
		}
		if !found {
			return []string{fmt.Sprintf("%s: %v is not one of %v", at, value, values)}
		}
	}

	var problems []string
	switch s["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: want an object, got %T", at, value)}
		}
		for _, name := range s["required"].([]any) {
			if _, ok := object[name.(string)]; !ok {
				problems = append(problems, fmt.Sprintf("%s: missing %s", at, name))
			}
		}
		properties, _ := s["properties"].(map[string]any)
		for name, field := range object {
			if fieldSchema, ok := properties[name]; ok {
				problems = append(problems, validate(spec, fieldSchema, field, at+"."+name)...)
			} else {
				problems = append(problems, fmt.Sprintf("%s: %s is not in the schema", at, name))
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: want an array, got %T", at, value)}
		}
		for i, item := range items {
			problems = append(problems, validate(spec, s["items"], item, fmt.Sprintf("%s[%d]", at, i))...)
		}
	case "string":
		if _, ok := value.(string); !ok {
			problems = append(problems, fmt.Sprintf("%s: want a string, got %T", at, value))
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			problems = append(problems, fmt.Sprintf("%s: want a boolean, got %T", at, value))
		}
	case "integer", "number":
		if _, ok := value.(float64); !ok {
			problems = append(problems, fmt.Sprintf("%s: want a number, got %T", at, value))
		}
	}
	return problems
}
