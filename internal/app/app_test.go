package app

import (
	"reflect"
	"slices"
	"testing"
)

// TestBindings pins the methods Wails binds: the frontend calls them by
// name, so adding or renaming one must be deliberate (and must not leak
// core lifecycle methods such as Start or Stop to the WebView).
func TestBindings(t *testing.T) {
	want := []string{
		"CancelToolInstall", "CheckAccount", "CheckForUpdates", "ChooseDownloadsDir",
		"ChooseToolPath", "ConnectBrowserProfile", "ConnectServer", "DeleteVideo", "DownloadVideo",
		"GetAccounts", "GetAppInfo", "GetConnection", "GetDependencies", "GetJobs", "GetLibrary", "GetLocalAccounts",
		"GetSettings", "GetStorageInfo", "GetUpdateInfo", "ImportCookiesFile",
		"InstallTools", "InstallUpdate", "OpenDownloadsDir", "OpenReleasePage",
		"ReconnectServer", "RefreshAccount", "RemoveAccount", "RemoveJob", "ResetAllData", "ResetToolPath",
		"RetryJob", "RevealVideo", "RollbackYtDlp", "SaveMediaFile", "SavePlaybackPosition", "ScanBrowsers",
		"SendAccountToServer", "SetMaxConcurrentDownloads", "SetProxy", "SetUpdateSettings", "UpdateYtDlp", "UseLocal",
	}

	typ := reflect.TypeOf(&App{})
	var got []string
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("bound methods changed:\n got %v\nwant %v", got, want)
	}
}
