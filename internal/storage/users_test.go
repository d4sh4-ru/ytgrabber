package storage

import (
	"errors"
	"testing"
	"time"

	"ytgrabber/internal/model"
)

func TestUsersDevicesCodes(t *testing.T) {
	db := openTestDB(t, "")
	admin := &model.User{ID: "local", Name: "Анна", Role: model.RoleAdmin, CreatedAt: "1"}
	if created, err := db.CreateFirstUser(admin); err != nil || !created {
		t.Fatalf("first user: %v %v", created, err)
	}
	if created, _ := db.CreateFirstUser(&model.User{ID: "x", Name: "Другой", Role: model.RoleAdmin}); created {
		t.Error("a second first user")
	}
	if err := db.CreateUser(&model.User{ID: "u2", Name: "АННА", Role: model.RoleUser}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("a name taken in another case: %v", err)
	}
	if err := db.CreateUser(&model.User{ID: "u2", Name: "Борис", Role: model.RoleUser, CreatedAt: "2"}); err != nil {
		t.Fatal(err)
	}

	if err := db.CreateDevice(&model.Device{ID: "d1", UserID: "u2", Name: "phone"}, "hash1"); err != nil {
		t.Fatal(err)
	}
	if device, err := db.DeviceByToken("hash1"); err != nil || device.UserID != "u2" {
		t.Errorf("device by token: %+v %v", device, err)
	}
	if err := db.DeleteDevice("local", "d1"); !errors.Is(err, ErrDeviceNotFound) {
		t.Errorf("revoking someone else's device: %v", err)
	}

	now := time.Unix(1000, 0)
	db.SaveCode(&model.Code{Hash: "c1", Kind: model.CodeLink, UserID: "u2", CreatedBy: "u2", ExpiresAt: now.Add(time.Minute)})
	db.SaveCode(&model.Code{Hash: "old", Kind: model.CodeInvite, Role: model.RoleUser, CreatedBy: "local", ExpiresAt: now})
	if _, err := db.PeekCode("old", now); !errors.Is(err, ErrCodeNotFound) {
		t.Errorf("an expired code: %v", err)
	}
	if code, err := db.PeekCode("c1", now); err != nil || code.UserID != "u2" {
		t.Errorf("peek: %+v %v", code, err)
	}
	if code, err := db.TakeCode("c1", now); err != nil || code.Kind != model.CodeLink || !code.ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Errorf("take: %+v %v", code, err)
	}
	if _, err := db.TakeCode("c1", now); !errors.Is(err, ErrCodeNotFound) {
		t.Errorf("a code taken twice: %v", err)
	}

	db.SaveCode(&model.Code{Hash: "c2", Kind: model.CodeLink, UserID: "u2", CreatedBy: "local", ExpiresAt: now.Add(time.Hour)})
	if err := db.DeleteUser("u2"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DeviceByToken("hash1"); !errors.Is(err, ErrDeviceNotFound) {
		t.Errorf("a removed user's device: %v", err)
	}
	if _, err := db.PeekCode("c2", now); !errors.Is(err, ErrCodeNotFound) {
		t.Errorf("a removed user's link: %v", err)
	}
	if users, _ := db.ListUsers(); len(users) != 1 || users[0].ID != "local" {
		t.Errorf("users = %+v", users)
	}
}
