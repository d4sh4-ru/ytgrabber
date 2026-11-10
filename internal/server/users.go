package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/skip2/go-qrcode"

	"ytgrabber/internal/core"
	"ytgrabber/internal/model"
)

// Devices: every user manages their own.

func (s *Server) listDevices(r *http.Request) (any, error) {
	db, err := s.auth.users()
	if err != nil {
		return nil, err
	}
	id := identityOf(r)
	devices, err := db.ListDevices(id.user.ID)
	for _, device := range devices {
		device.Current = device.ID == id.device.ID
	}
	return devices, err
}

// createDevice makes a token for an API client or a script: a device
// like any other, revoked the same way.
func (s *Server) createDevice(r *http.Request, body *deviceRequest) (any, error) {
	db, err := s.auth.users()
	if err != nil {
		return nil, err
	}
	device, token, err := s.auth.newDevice(db, identityOf(r).user, body.Name)
	if err != nil {
		return nil, err
	}
	return newDeviceResponse{Device: device, Token: token}, nil
}

// removeDevice revokes one of the user's devices: its token stops working
// and its open event stream is closed.
func (s *Server) removeDevice(r *http.Request) (any, error) {
	db, err := s.auth.users()
	if err != nil {
		return nil, err
	}
	id := r.PathValue("id")
	if err := db.DeleteDevice(identityOf(r).user.ID, id); err != nil {
		return nil, err
	}
	s.Events.closeDevice(id)
	return nil, nil
}

// linkDevice makes a code that signs another device in as this user.
func (s *Server) linkDevice(r *http.Request) (any, error) {
	db, err := s.auth.users()
	if err != nil {
		return nil, err
	}
	user := identityOf(r).user
	return s.auth.issueCode(db, model.Code{Kind: model.CodeLink, UserID: user.ID, CreatedBy: user.ID}, ownLinkTTL)
}

// Users: the admin's.

func (s *Server) listUsers(r *http.Request) (any, error) {
	db, err := s.auth.users()
	if err != nil {
		return nil, err
	}
	users, err := db.ListUsers()
	if err != nil {
		return nil, err
	}
	me := identityOf(r).user.ID
	result := make([]userInfo, 0, len(users))
	for _, user := range users {
		devices, err := db.ListDevices(user.ID)
		if err != nil {
			return nil, err
		}
		info := userInfo{ID: user.ID, Name: user.Name, Role: user.Role, CreatedAt: user.CreatedAt, Devices: len(devices), Current: user.ID == me}
		for _, device := range devices {
			info.LastSeenAt = max(info.LastSeenAt, device.LastSeenAt)
		}
		result = append(result, info)
	}
	return result, nil
}

func (s *Server) inviteUser(r *http.Request, body *inviteRequest) (any, error) {
	role := model.Role(body.Role)
	if role == "" {
		role = model.RoleUser
	}
	if role != model.RoleUser && role != model.RoleAdmin {
		return nil, badRequest("роль: admin или user")
	}
	db, err := s.auth.users()
	if err != nil {
		return nil, err
	}
	return s.auth.issueCode(db, model.Code{Kind: model.CodeInvite, Role: role, CreatedBy: identityOf(r).user.ID}, sentCodeTTL)
}

// linkUser makes a device link for someone else, for when they've lost
// every device they were signed in on.
func (s *Server) linkUser(r *http.Request) (any, error) {
	db, err := s.auth.users()
	if err != nil {
		return nil, err
	}
	user, err := db.GetUser(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return s.auth.issueCode(db, model.Code{Kind: model.CodeLink, UserID: user.ID, CreatedBy: identityOf(r).user.ID}, sentCodeTTL)
}

// removeUser deletes a user with everything they have. An admin can't
// remove themselves, so there is always one admin left.
func (s *Server) removeUser(r *http.Request) (any, error) {
	db, err := s.auth.users()
	if err != nil {
		return nil, err
	}
	id := r.PathValue("id")
	if id == identityOf(r).user.ID {
		return nil, badRequest("нельзя удалить самого себя")
	}
	user, err := db.GetUser(id)
	if err != nil {
		return nil, err
	}
	// Signed out first, so nothing new is added while their data goes.
	if err := db.DeleteUser(user.ID); err != nil {
		return nil, err
	}
	s.Events.closeUser(core.UserID(user.ID))
	if err := s.app.DeleteUserData(core.UserID(user.ID)); err != nil {
		return nil, fmt.Errorf("пользователь удалён, но не все его данные: %w", err)
	}
	return nil, nil
}

// maxQRText bounds what /qr encodes: a link with a code is far shorter.
const maxQRText = 512

// serveQR draws text as a QR code in SVG, for a phone to scan a device
// link or an invite off the screen. Drawn here so the frontend needs no
// QR library.
func serveQR(w http.ResponseWriter, r *http.Request) {
	text := r.URL.Query().Get("text")
	if text == "" || len(text) > maxQRText {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "нужен text, не длиннее 512 байт"})
		return
	}
	code, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(qrSVG(code.Bitmap())))
}

// qrSVG draws the modules as one path, a unit square each; the bitmap
// already has the quiet zone around it.
func qrSVG(bitmap [][]bool) string {
	size := len(bitmap)
	var path strings.Builder
	for y, row := range bitmap {
		for x, dark := range row {
			if dark {
				fmt.Fprintf(&path, "M%d %dh1v1h-1z", x, y)
			}
		}
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+
		`<rect width="100%%" height="100%%" fill="#fff"/><path d="%s" fill="#000"/></svg>`, size, size, path.String())
}
