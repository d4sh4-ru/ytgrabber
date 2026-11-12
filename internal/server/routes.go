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
	admin   bool // only for admins

	query    []queryParam
	request  any  // pointer to the JSON body type; nil: no body
	upload   bool // multipart/form-data with "site" and "file" instead of JSON
	response any  // the JSON response type; nil: 204 No Content
	svg      bool // answers with an SVG image instead

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

type proxyRequest struct {
	// Proxy is like socks5://host:1080 or http://user:pass@host:3128;
	// empty turns the proxy off.
	Proxy string `json:"proxy"`
}

type installRequest struct {
	// Names lists tools to install; empty installs every missing one.
	Names []string `json:"names"`
}

type codeRequest struct {
	Code string `json:"code"`
}

type loginRequest struct {
	// Code is the setup code, an invite or a device link.
	Code string `json:"code"`
	// Name is the new user's, for the setup code and invites.
	Name string `json:"name,omitempty"`
	// DeviceName tells this device apart in the list of devices.
	DeviceName string `json:"deviceName,omitempty"`
}

type deviceRequest struct {
	Name string `json:"name"`
}

type inviteRequest struct {
	Role string `json:"role,omitempty" enum:"admin,user"`
}

// Responses without a core type.

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type sessionResponse struct {
	Authenticated bool `json:"authenticated"`
	// SetupRequired: the server has no users yet, the setup code makes
	// the first admin.
	SetupRequired bool        `json:"setupRequired,omitempty"`
	User          *model.User `json:"user,omitempty"`
	DeviceID      string      `json:"deviceId,omitempty"`
}

type codeResponse struct {
	Kind model.CodeKind `json:"kind" enum:"setup,invite,link"`
	// UserName is who a device link signs in as.
	UserName string `json:"userName,omitempty"`
}

// codeIssued is a one-time code as the user types it.
type codeIssued struct {
	Code      string `json:"code"`
	ExpiresAt string `json:"expiresAt"`
}

type newDeviceResponse struct {
	Device *model.Device `json:"device"`
	// Token is shown this once: the server keeps only its hash.
	Token string `json:"token"`
}

type userInfo struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Role       model.Role `json:"role" enum:"admin,user"`
	CreatedAt  string     `json:"createdAt"`
	Devices    int        `json:"devices"`
	LastSeenAt string     `json:"lastSeenAt"`
	// Current is the admin looking at the list.
	Current bool `json:"current"`
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

		// Session: the login screen trades a one-time code for a device.
		{
			method: "GET", path: "/session", summary: "Кто вошёл; нужна ли первая настройка", public: true,
			response: sessionResponse{}, raw: s.auth.getSession,
		},
		{
			method: "POST", path: "/session/code", summary: "Что сделает код (не расходуя его)", public: true,
			request: &codeRequest{}, response: codeResponse{}, raw: s.auth.checkCode,
			doc: "Код настройки, приглашение или код устройства. Неверные коды ограничены: " +
				"10 в минуту на сервер, дальше 429.",
		},
		{
			method: "POST", path: "/session", summary: "Войти по коду: новое устройство и cookie сессии", public: true,
			request: &loginRequest{}, response: sessionResponse{}, raw: s.auth.login,
			doc: "Код настройки создаёт первого администратора, приглашение — нового пользователя " +
				"(нужно name), код устройства входит как тот, кто его создал. Код одноразовый.",
		},
		{
			method: "DELETE", path: "/session", summary: "Выйти: отзывает это устройство", public: true,
			raw: s.auth.logout,
		},

		// Devices: the user's own.
		{
			method: "GET", path: "/devices", summary: "Мои устройства",
			response: []*model.Device{}, handle: s.listDevices,
		},
		{
			method: "POST", path: "/devices", summary: "Токен для скрипта или API-клиента",
			request: &deviceRequest{}, response: newDeviceResponse{}, handle: jsonBody(s.createDevice),
		},
		{
			method: "DELETE", path: "/devices/{id}", summary: "Отозвать устройство: выходит и теряет события",
			handle: s.removeDevice,
		},
		{
			method: "POST", path: "/devices/link", summary: "Код, которым войти на другом устройстве (10 минут)",
			response: codeIssued{}, handle: s.linkDevice,
		},
		{
			method: "GET", path: "/qr", summary: "QR-код текста (ссылки с кодом) в SVG",
			query: []queryParam{{name: "text", description: "что закодировать, до 512 байт"}},
			svg:   true, raw: serveQR,
		},

		// Users: the admin's.
		{
			method: "GET", path: "/users", summary: "Пользователи", admin: true,
			response: []userInfo{}, handle: s.listUsers,
		},
		{
			method: "POST", path: "/users/invites", summary: "Приглашение нового пользователя (сутки)", admin: true,
			request: &inviteRequest{}, response: codeIssued{}, handle: jsonBody(s.inviteUser),
		},
		{
			method: "POST", path: "/users/{id}/link", summary: "Код входа для пользователя, потерявшего устройства (сутки)", admin: true,
			response: codeIssued{}, handle: s.linkUser,
		},
		{
			method: "DELETE", path: "/users/{id}", summary: "Удалить пользователя со всеми его данными", admin: true,
			handle: s.removeUser,
		},

		{
			method: "GET", path: "/events", summary: "События (WebSocket)", raw: s.serveEvents,
			doc: "WebSocket. Каждое сообщение — JSON {\"name\": ..., \"data\": ...}: " +
				"download-status и download-progress (model.DownloadJob), job-removed (id задачи), " +
				"library-changed, accounts-changed, tool-install-progress. " +
				"События о загрузках, библиотеке и аккаунтах приходят только их владельцу. " +
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
			handle:   func(r *http.Request) (any, error) { return s.app.GetJobs(s.userOf(r)) },
		},
		{
			method: "POST", path: "/jobs", summary: "Поставить ссылку в очередь; возвращает id задачи",
			request: &downloadRequest{}, response: "",
			handle: jsonBody(func(r *http.Request, body *downloadRequest) (any, error) {
				return s.app.DownloadVideo(s.userOf(r), body.URL, body.Quality, body.UseAccount)
			}),
		},
		{
			method: "POST", path: "/jobs/{id}/retry", summary: "Повторить задачу с ошибкой",
			request: &retryRequest{},
			handle: jsonBody(func(r *http.Request, body *retryRequest) (any, error) {
				return nil, s.app.RetryJob(s.userOf(r), r.PathValue("id"), body.UseAccount)
			}),
		},
		{
			method: "DELETE", path: "/jobs/{id}", summary: "Отменить или убрать задачу",
			handle: func(r *http.Request) (any, error) {
				return nil, s.app.RemoveJob(s.userOf(r), r.PathValue("id"))
			},
		},

		// Library.
		{
			method: "GET", path: "/library", summary: "Скачанные видео и аудио",
			response: []*model.VideoRecord{},
			handle:   func(r *http.Request) (any, error) { return s.app.GetLibrary(s.userOf(r)) },
		},
		{
			// Files are always deleted with the record: a file nobody's
			// library lists would only fill the server's disk.
			method: "DELETE", path: "/library/{id}", summary: "Удалить из библиотеки вместе с файлами",
			handle: func(r *http.Request) (any, error) {
				return nil, s.app.DeleteVideo(s.userOf(r), r.PathValue("id"), true)
			},
		},
		{
			method: "PUT", path: "/library/{id}/position",
			summary:  "Запомнить место просмотра; возвращает сохранённое (0 у начала и конца)",
			request:  &positionRequest{},
			response: float64(0),
			handle: jsonBody(func(r *http.Request, body *positionRequest) (any, error) {
				return s.app.SavePlaybackPosition(s.userOf(r), r.PathValue("id"), body.Seconds)
			}),
		},

		// Site accounts. Browsers aren't scanned on a server: cookies come
		// as an uploaded cookies.txt.
		{
			method: "GET", path: "/accounts", summary: "Подключённые сайты",
			response: []*model.Account{},
			handle:   func(r *http.Request) (any, error) { return s.app.GetAccounts(s.userOf(r)) },
		},
		{
			method: "POST", path: "/accounts", summary: "Подключить сайт из cookies.txt",
			upload: true, response: &model.Account{}, handle: s.importCookies,
		},
		{
			method: "POST", path: "/accounts/{id}/check", summary: "Проверить, действует ли вход",
			response: &model.Account{},
			handle: func(r *http.Request) (any, error) {
				return s.app.CheckAccount(s.userOf(r), r.PathValue("id"))
			},
		},
		{
			method: "DELETE", path: "/accounts/{id}", summary: "Отключить сайт и удалить cookies",
			handle: func(r *http.Request) (any, error) {
				return nil, s.app.RemoveAccount(s.userOf(r), r.PathValue("id"))
			},
		},

		// Settings that make sense on a server: the downloads folder and
		// tool paths are fixed by its configuration.
		{
			method: "GET", path: "/settings", summary: "Настройки",
			response: core.Settings{},
			handle:   func(r *http.Request) (any, error) { return s.app.GetSettings(s.userOf(r)), nil },
		},
		{
			method: "PUT", path: "/settings/max-concurrent-downloads", summary: "Сколько загрузок идёт одновременно", admin: true,
			request: &intValueRequest{}, response: core.Settings{},
			handle: jsonBody(func(_ *http.Request, body *intValueRequest) (any, error) {
				return s.app.SetMaxConcurrentDownloads(body.Value)
			}),
		},
		{
			method: "PUT", path: "/settings/proxy", summary: "Прокси для yt-dlp (пароль в ответах скрыт)", admin: true,
			request: &proxyRequest{}, response: core.Settings{},
			handle: jsonBody(func(_ *http.Request, body *proxyRequest) (any, error) {
				return s.app.SetProxy(body.Proxy)
			}),
		},

		// Tools.
		{
			method: "GET", path: "/tools", summary: "yt-dlp, ffmpeg, deno: найдены ли и какие версии",
			response: tools.Report{},
			handle:   func(*http.Request) (any, error) { return s.app.GetDependencies(), nil },
		},
		{
			method: "POST", path: "/tools/install", summary: "Установить программы; прогресс — событиями tool-install-progress", admin: true,
			request: &installRequest{}, response: tools.Report{},
			handle: jsonBody(func(_ *http.Request, body *installRequest) (any, error) {
				return s.app.InstallTools(body.Names)
			}),
		},
		{
			method: "POST", path: "/tools/install/cancel", summary: "Прервать установку", admin: true,
			handle: func(*http.Request) (any, error) {
				s.app.CancelToolInstall()
				return nil, nil
			},
		},
		{
			method: "POST", path: "/tools/yt-dlp/rollback", admin: true,
			summary:  "Вернуть версию yt-dlp, которую заменило последнее обновление",
			response: tools.Report{},
			handle:   func(*http.Request) (any, error) { return s.app.RollbackYtDlp() },
		},
		{
			method: "POST", path: "/tools/yt-dlp/update", summary: "Обновить yt-dlp; возвращает, что получилось", admin: true,
			response: "",
			handle:   func(*http.Request) (any, error) { return s.app.UpdateYtDlp() },
		},
	}
}
