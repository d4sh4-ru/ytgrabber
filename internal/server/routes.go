package server

import (
	"net/http"

	"ytgrabber/internal/core"
	"ytgrabber/internal/model"
	"ytgrabber/internal/tools"
)

// apiPrefix is where the JSON API lives; route paths are relative to it.
const apiPrefix = "/api/v1"

// route is one endpoint of the API. The table in routes() is what both
// Handler serves and openapi.json describes (see openapi.go), so the
// spec can't drift from the code: request and response types are the
// Go types the handler really decodes and returns.
type route struct {
	method  string
	path    string // relative to apiPrefix, "{name}" for path parameters
	summary string
	public  bool // reachable without a token

	query    []queryParam
	request  any  // pointer to the JSON body type; nil: no body
	upload   bool // multipart/form-data with "site" and "file" instead of JSON
	response any  // the JSON response type; nil: 204 No Content

	handle func(*http.Request) (any, error) // JSON endpoints
	raw    http.HandlerFunc                 // everything else
	doc    string                           // what raw does, for the spec
}

type queryParam struct {
	name        string
	description string
}

// Request bodies.

type downloadRequest struct {
	URL        string `json:"url"`
	Quality    string `json:"quality" enum:"best,1080p,720p,audio"`
	UseAccount bool   `json:"useAccount"`
}

type retryRequest struct {
	UseAccount bool `json:"useAccount"`
}

type positionRequest struct {
	Seconds float64 `json:"seconds"`
}

type intValueRequest struct {
	Value int `json:"value"`
}

type installRequest struct {
	// Names lists tools to install; empty installs every missing one.
	Names []string `json:"names"`
}

type loginRequest struct {
	Token string `json:"token"`
}

// Responses without a core type.

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type sessionResponse struct {
	Authenticated bool `json:"authenticated"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// jsonBody decodes the request body into a new T and passes it on.
func jsonBody[T any](next func(*http.Request, *T) (any, error)) func(*http.Request) (any, error) {
	return func(r *http.Request) (any, error) {
		body := new(T)
		if err := decodeJSON(r, body); err != nil {
			return nil, err
		}
		return next(r, body)
	}
}

func (s *Server) routes() []route {
	return []route{
		{
			method: "GET", path: "/health", summary: "Сервер работает", public: true,
			response: healthResponse{},
			handle: func(*http.Request) (any, error) {
				return healthResponse{Status: "ok", Version: core.Version}, nil
			},
		},

		// Session: the login screen trades the token for a cookie.
		{
			method: "GET", path: "/session", summary: "Вошёл ли клиент", public: true,
			response: sessionResponse{}, raw: s.auth.getSession,
		},
		{
			method: "POST", path: "/session", summary: "Войти по токену: ставит cookie сессии", public: true,
			request: &loginRequest{}, response: sessionResponse{}, raw: s.auth.login,
		},
		{
			method: "DELETE", path: "/session", summary: "Выйти: удаляет cookie сессии", public: true,
			raw: s.auth.logout,
		},

		{
			method: "GET", path: "/events", summary: "События (WebSocket)", raw: s.Events.serveWS,
			doc: "WebSocket. Каждое сообщение — JSON {\"name\": ..., \"data\": ...}: " +
				"download-status и download-progress (model.DownloadJob), job-removed (id задачи), " +
				"library-changed, accounts-changed, tool-install-progress. " +
				"После переподключения состояние нужно перечитать: пропущенные события не повторяются.",
		},

		{
			method: "GET", path: "/info", summary: "Версия, пути и ошибка запуска",
			response: core.AppInfo{},
			handle:   func(*http.Request) (any, error) { return s.app.GetAppInfo(), nil },
		},

		// Downloads.
		{
			method: "GET", path: "/jobs", summary: "Очередь загрузок",
			response: []model.DownloadJob{},
			handle:   func(*http.Request) (any, error) { return s.app.GetJobs(user) },
		},
		{
			method: "POST", path: "/jobs", summary: "Поставить ссылку в очередь; возвращает id задачи",
			request: &downloadRequest{}, response: "",
			handle: jsonBody(func(_ *http.Request, body *downloadRequest) (any, error) {
				return s.app.DownloadVideo(user, body.URL, body.Quality, body.UseAccount)
			}),
		},
		{
			method: "POST", path: "/jobs/{id}/retry", summary: "Повторить задачу с ошибкой",
			request: &retryRequest{},
			handle: jsonBody(func(r *http.Request, body *retryRequest) (any, error) {
				return nil, s.app.RetryJob(user, r.PathValue("id"), body.UseAccount)
			}),
		},
		{
			method: "DELETE", path: "/jobs/{id}", summary: "Отменить или убрать задачу",
			handle: func(r *http.Request) (any, error) {
				return nil, s.app.RemoveJob(user, r.PathValue("id"))
			},
		},

		// Library.
		{
			method: "GET", path: "/library", summary: "Скачанные видео и аудио",
			response: []*model.VideoRecord{},
			handle:   func(*http.Request) (any, error) { return s.app.GetLibrary(user) },
		},
		{
			method: "DELETE", path: "/library/{id}", summary: "Убрать из библиотеки",
			query: []queryParam{{name: "deleteFiles", description: "true — удалить и файлы с диска"}},
			handle: func(r *http.Request) (any, error) {
				return nil, s.app.DeleteVideo(user, r.PathValue("id"), r.URL.Query().Get("deleteFiles") == "true")
			},
		},
		{
			method: "PUT", path: "/library/{id}/position",
			summary:  "Запомнить место просмотра; возвращает сохранённое (0 у начала и конца)",
			request:  &positionRequest{},
			response: float64(0),
			handle: jsonBody(func(r *http.Request, body *positionRequest) (any, error) {
				return s.app.SavePlaybackPosition(user, r.PathValue("id"), body.Seconds)
			}),
		},

		// Site accounts. Browsers aren't scanned on a server: cookies come
		// as an uploaded cookies.txt.
		{
			method: "GET", path: "/accounts", summary: "Подключённые сайты",
			response: []*model.Account{},
			handle:   func(*http.Request) (any, error) { return s.app.GetAccounts(user) },
		},
		{
			method: "POST", path: "/accounts", summary: "Подключить сайт из cookies.txt",
			upload: true, response: &model.Account{}, handle: s.importCookies,
		},
		{
			method: "POST", path: "/accounts/{id}/check", summary: "Проверить, действует ли вход",
			response: &model.Account{},
			handle: func(r *http.Request) (any, error) {
				return s.app.CheckAccount(user, r.PathValue("id"))
			},
		},
		{
			method: "DELETE", path: "/accounts/{id}", summary: "Отключить сайт и удалить cookies",
			handle: func(r *http.Request) (any, error) {
				return nil, s.app.RemoveAccount(user, r.PathValue("id"))
			},
		},

		// Settings that make sense on a server: the downloads folder and
		// tool paths are fixed by its configuration.
		{
			method: "GET", path: "/settings", summary: "Настройки",
			response: core.Settings{},
			handle:   func(*http.Request) (any, error) { return s.app.GetSettings(user), nil },
		},
		{
			method: "PUT", path: "/settings/max-concurrent-downloads", summary: "Сколько загрузок идёт одновременно",
			request: &intValueRequest{}, response: core.Settings{},
			handle: jsonBody(func(_ *http.Request, body *intValueRequest) (any, error) {
				return s.app.SetMaxConcurrentDownloads(body.Value)
			}),
		},

		// Tools.
		{
			method: "GET", path: "/tools", summary: "yt-dlp, ffmpeg, deno: найдены ли и какие версии",
			response: tools.Report{},
			handle:   func(*http.Request) (any, error) { return s.app.GetDependencies(), nil },
		},
		{
			method: "POST", path: "/tools/install", summary: "Установить программы; прогресс — событиями tool-install-progress",
			request: &installRequest{}, response: tools.Report{},
			handle: jsonBody(func(_ *http.Request, body *installRequest) (any, error) {
				return s.app.InstallTools(body.Names)
			}),
		},
		{
			method: "POST", path: "/tools/install/cancel", summary: "Прервать установку",
			handle: func(*http.Request) (any, error) {
				s.app.CancelToolInstall()
				return nil, nil
			},
		},
		{
			method: "POST", path: "/tools/yt-dlp/rollback",
			summary:  "Вернуть версию yt-dlp, которую заменило последнее обновление",
			response: tools.Report{},
			handle:   func(*http.Request) (any, error) { return s.app.RollbackYtDlp() },
		},
		{
			method: "POST", path: "/tools/yt-dlp/update", summary: "Обновить yt-dlp; возвращает, что получилось",
			response: "",
			handle:   func(*http.Request) (any, error) { return s.app.UpdateYtDlp() },
		},
	}
}
