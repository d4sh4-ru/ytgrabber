package server

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"ytgrabber/internal/model"
)

// openapi.json describes the API for clients; the frontend's TypeScript
// client is generated from it (frontend: npm run generate:api). It is
// built from the route table by Spec and checked in; TestOpenAPISpec
// fails when it is stale, and regenerates it with:
//
//	go test ./internal/server -run TestOpenAPISpec -update
//
//go:embed openapi.json
var specJSON []byte

func serveSpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(specJSON)
}

// enums lists the values of the string types the API uses as enums: Go
// has no way to find a type's constants by reflection.
var enums = map[reflect.Type][]string{
	reflect.TypeFor[model.DownloadStatus](): {
		string(model.StatusPending), string(model.StatusDownloading), string(model.StatusProcessing),
		string(model.StatusDone), string(model.StatusError),
	},
	reflect.TypeFor[model.TrackKind]():     {string(model.TrackVideo), string(model.TrackAudio)},
	reflect.TypeFor[model.ErrorKind]():     {string(model.ErrorGeneric), string(model.ErrorLoginRequired)},
	reflect.TypeFor[model.AccountSource](): {string(model.AccountFromFile), string(model.AccountFromBrowser)},
	reflect.TypeFor[model.AccountStatus](): {
		string(model.AccountOK), string(model.AccountExpired), string(model.AccountError),
	},
	reflect.TypeFor[model.MediaKind](): {string(model.MediaVideo), string(model.MediaAudio)},
}

type schema = map[string]any

// specBuilder turns Go types into JSON Schema. Types of other packages
// become named components ("model.DownloadJob", the names the Wails
// bindings use too); the server's own request types are inlined.
type specBuilder struct {
	components map[string]schema
}

func (b *specBuilder) schemaOf(t reflect.Type) schema {
	if values, ok := enums[t]; ok {
		return schema{"type": "string", "enum": values}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return b.schemaOf(t.Elem())
	case reflect.String:
		return schema{"type": "string"}
	case reflect.Bool:
		return schema{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return schema{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return schema{"type": "number"}
	case reflect.Slice, reflect.Array:
		return schema{"type": "array", "items": b.schemaOf(t.Elem())}
	case reflect.Map:
		return schema{"type": "object", "additionalProperties": b.schemaOf(t.Elem())}
	case reflect.Struct:
		if t.PkgPath() == reflect.TypeFor[Server]().PkgPath() || t.Name() == "" {
			return b.structSchema(t)
		}
		name := componentName(t)
		if _, ok := b.components[name]; !ok {
			b.components[name] = nil // reserve: the struct may refer to itself
			b.components[name] = b.structSchema(t)
		}
		return schema{"$ref": "#/components/schemas/" + name}
	}
	return schema{}
}

func componentName(t reflect.Type) string {
	pkg := t.PkgPath()
	return pkg[strings.LastIndex(pkg, "/")+1:] + "." + t.Name()
}

// structSchema follows encoding/json: the json tag names a field, "-"
// hides it, a field without omitempty is always present and an untagged
// embedded struct contributes its own fields.
func (b *specBuilder) structSchema(t reflect.Type) schema {
	properties := schema{}
	required := []string{}
	b.addFields(t, properties, &required)
	return schema{"type": "object", "properties": properties, "required": required}
}

func (b *specBuilder) addFields(t reflect.Type, properties schema, required *[]string) {
	for i := range t.NumField() {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if field.Anonymous && tag == "" && field.Type.Kind() == reflect.Struct {
			b.addFields(field.Type, properties, required)
			continue
		}
		if !field.IsExported() {
			continue
		}
		if tag == "-" {
			continue
		}
		name, options, _ := strings.Cut(tag, ",")
		if name == "" {
			name = field.Name
		}
		property := b.schemaOf(field.Type)
		if values := field.Tag.Get("enum"); values != "" {
			property = schema{"type": "string", "enum": strings.Split(values, ",")}
		}
		properties[name] = property
		if !strings.Contains(options, "omitempty") {
			*required = append(*required, name)
		}
	}
}

var pathParamRe = regexp.MustCompile(`\{(\w+)\}`)

// Spec builds the OpenAPI document for routes.
func Spec(routes []route) ([]byte, error) {
	b := &specBuilder{components: map[string]schema{}}
	errorContent := schema{"application/json": schema{"schema": b.schemaOf(reflect.TypeFor[errorResponse]())}}

	paths := map[string]schema{}
	for _, rt := range routes {
		op := schema{
			"operationId": operationID(rt),
			"summary":     rt.summary,
		}
		if rt.doc != "" {
			op["description"] = rt.doc
		}
		if rt.public {
			op["security"] = []any{}
		}

		var params []any
		for _, match := range pathParamRe.FindAllStringSubmatch(rt.path, -1) {
			params = append(params, schema{
				"name": match[1], "in": "path", "required": true, "schema": schema{"type": "string"},
			})
		}
		for _, q := range rt.query {
			params = append(params, schema{
				"name": q.name, "in": "query", "description": q.description, "schema": schema{"type": "string"},
			})
		}
		if params != nil {
			op["parameters"] = params
		}

		switch {
		case rt.upload:
			op["requestBody"] = schema{"required": true, "content": schema{"multipart/form-data": schema{"schema": schema{
				"type": "object",
				"properties": schema{
					"site": schema{"type": "string", "description": "ссылка или домен сайта"},
					"file": schema{"type": "string", "format": "binary", "description": "cookies.txt"},
				},
				"required": []string{"site", "file"},
			}}}}
		case rt.request != nil:
			op["requestBody"] = schema{"required": true, "content": schema{
				"application/json": schema{"schema": b.schemaOf(reflect.TypeOf(rt.request))},
			}}
		}

		responses := schema{}
		if rt.response != nil {
			responses["200"] = schema{"description": "OK", "content": schema{
				"application/json": schema{"schema": b.schemaOf(reflect.TypeOf(rt.response))},
			}}
		} else {
			responses["204"] = schema{"description": "Готово"}
		}
		if rt.handle != nil || rt.method == "POST" {
			responses["400"] = schema{"description": "Ошибка; текст для пользователя", "content": errorContent}
		}
		if !rt.public {
			responses["401"] = schema{"description": "Нужен вход", "content": errorContent}
		}
		op["responses"] = responses

		if paths[rt.path] == nil {
			paths[rt.path] = schema{}
		}
		paths[rt.path][strings.ToLower(rt.method)] = op
	}

	// Library files are outside the API prefix.
	paths["/media/{id}/{file}"] = schema{
		"servers": []any{schema{"url": "/"}},
		"get": schema{
			"operationId": "getMedia",
			"summary":     "Файл из библиотеки: mediaUrl и thumbnailUrl в model.VideoRecord",
			"description": "Поддерживает Range для перемотки. С ?download=1 отдаётся вложением под своим именем.",
			"parameters": []any{
				schema{"name": "id", "in": "path", "required": true, "schema": schema{"type": "string"}},
				schema{"name": "file", "in": "path", "required": true, "schema": schema{"type": "string"}},
				schema{"name": "download", "in": "query", "schema": schema{"type": "string", "enum": []string{"1"}}},
			},
			"responses": schema{
				"200": schema{"description": "Файл", "content": schema{"application/octet-stream": schema{
					"schema": schema{"type": "string", "format": "binary"},
				}}},
				"206": schema{"description": "Часть файла (Range)"},
				"404": schema{"description": "Нет такого файла"},
			},
		},
	}

	doc := schema{
		"openapi": "3.1.0",
		"info": schema{
			"title":   "ytgrabber-server",
			"version": "1",
			"description": "API self-hosted сервера ytgrabber. Авторизация: заголовок " +
				"«Authorization: Bearer <токен>» или cookie сессии после POST /session.",
		},
		"servers":  []any{schema{"url": apiPrefix}},
		"security": []any{schema{"bearer": []string{}}, schema{"session": []string{}}},
		"paths":    paths,
		"components": schema{
			"schemas": b.components,
			"securitySchemes": schema{
				"bearer":  schema{"type": "http", "scheme": "bearer"},
				"session": schema{"type": "apiKey", "in": "cookie", "name": sessionCookie},
			},
		},
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// operationID names an operation after its method and path:
// "POST /jobs/{id}/retry" → "postJobsIdRetry".
func operationID(rt route) string {
	var id strings.Builder
	id.WriteString(strings.ToLower(rt.method))
	for _, part := range strings.FieldsFunc(rt.path, func(r rune) bool {
		return r == '/' || r == '{' || r == '}' || r == '-'
	}) {
		id.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return id.String()
}
