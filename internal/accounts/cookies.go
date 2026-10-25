package accounts

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Cookie is one line of a Netscape cookies.txt file — the format yt-dlp
// reads with --cookies and writes back on exit, and browser extensions
// export.
type Cookie struct {
	Domain            string
	IncludeSubdomains bool
	Path              string
	Secure            bool
	Expires           int64 // unix seconds; 0 is a session cookie
	Name              string
	Value             string
	HTTPOnly          bool
}

// key identifies a cookie the way a browser does: the same name may exist
// for several domains and paths.
func (c Cookie) key() string {
	return strings.ToLower(c.Domain) + "\t" + c.Path + "\t" + c.Name
}

func (c Cookie) expired(now time.Time) bool {
	return c.Expires > 0 && c.Expires < now.Unix()
}

const (
	netscapeHeader = "# Netscape HTTP Cookie File"
	httpOnlyPrefix = "#HttpOnly_"
	// maxCookiesFileSize keeps a wrong file (a video picked by mistake)
	// from being read into memory whole.
	maxCookiesFileSize = 16 << 20
)

var ErrNotCookiesFile = errors.New("файл не похож на cookies.txt (формат Netscape)")

// ParseCookies reads a cookies.txt file. Comment and malformed lines are
// skipped — exporters disagree on details — but a file without a single
// valid line is rejected.
func ParseCookies(r io.Reader) ([]Cookie, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxCookiesFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCookiesFileSize {
		return nil, ErrNotCookiesFile
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	var cookies []Cookie
	sawHeader := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), maxCookiesFileSize)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.HasPrefix(line, "# Netscape HTTP Cookie File") || strings.HasPrefix(line, "# HTTP Cookie File") {
			sawHeader = true
			continue
		}
		cookie, ok := parseLine(line)
		if ok {
			cookies = append(cookies, cookie)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(cookies) == 0 && !sawHeader {
		return nil, ErrNotCookiesFile
	}
	return cookies, nil
}

func parseLine(line string) (Cookie, bool) {
	httpOnly := false
	if strings.HasPrefix(line, httpOnlyPrefix) {
		httpOnly = true
		line = strings.TrimPrefix(line, httpOnlyPrefix)
	}
	if line == "" || strings.HasPrefix(line, "#") {
		return Cookie{}, false
	}

	fields := strings.Split(line, "\t")
	// Some exporters drop the trailing tab of an empty value.
	if len(fields) == 6 {
		fields = append(fields, "")
	}
	if len(fields) != 7 {
		return Cookie{}, false
	}

	domain := strings.TrimSpace(fields[0])
	name := fields[5]
	if domain == "" || name == "" {
		return Cookie{}, false
	}
	expires, err := strconv.ParseInt(strings.TrimSpace(fields[4]), 10, 64)
	if err != nil {
		// Some exporters write fractional seconds.
		value, floatErr := strconv.ParseFloat(strings.TrimSpace(fields[4]), 64)
		if floatErr != nil {
			return Cookie{}, false
		}
		expires = int64(value)
	}
	path := fields[2]
	if path == "" {
		path = "/"
	}

	return Cookie{
		Domain:            domain,
		IncludeSubdomains: strings.EqualFold(fields[1], "TRUE"),
		Path:              path,
		Secure:            strings.EqualFold(fields[3], "TRUE"),
		Expires:           max(expires, 0),
		Name:              name,
		Value:             fields[6],
		HTTPOnly:          httpOnly,
	}, true
}

// FormatCookies writes cookies in the format ParseCookies and yt-dlp read.
func FormatCookies(cookies []Cookie) []byte {
	var buf bytes.Buffer
	buf.WriteString(netscapeHeader + "\n\n")
	for _, c := range cookies {
		if c.HTTPOnly {
			buf.WriteString(httpOnlyPrefix)
		}
		fmt.Fprintf(&buf, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			c.Domain, netscapeBool(c.IncludeSubdomains), c.Path, netscapeBool(c.Secure), c.Expires, c.Name, c.Value)
	}
	return buf.Bytes()
}

func netscapeBool(value bool) string {
	if value {
		return "TRUE"
	}
	return "FALSE"
}

// FilterCookies keeps the unexpired cookies that belong to site (see
// Belongs); later duplicates of the same cookie replace earlier ones.
// It is applied to everything that enters a snapshot — an imported file,
// yt-dlp's write-back — so cookies of other sites never get stored.
func FilterCookies(cookies []Cookie, site string, now time.Time) []Cookie {
	index := map[string]int{}
	var kept []Cookie
	for _, c := range cookies {
		if c.expired(now) || !Belongs(c.Domain, site) || strings.ContainsAny(c.Name+c.Value+c.Path, "\t\r\n") {
			continue
		}
		if i, ok := index[c.key()]; ok {
			kept[i] = c
			continue
		}
		index[c.key()] = len(kept)
		kept = append(kept, c)
	}
	return kept
}

// MergeCookies folds the cookies one download ended with (updated) into
// the current snapshot, three-way: base is what that download started
// with, so only its own changes are applied. A cookie it changed or added
// is taken from updated; one in base but missing from updated was deleted
// by the site; anything it left as it was keeps its current value, which
// another download running in parallel may have refreshed meanwhile.
func MergeCookies(base, current, updated []Cookie) []Cookie {
	inBase := map[string]Cookie{}
	for _, c := range base {
		inBase[c.key()] = c
	}
	changed := map[string]Cookie{}
	inUpdated := map[string]bool{}
	for _, c := range updated {
		inUpdated[c.key()] = true
		if old, ok := inBase[c.key()]; !ok || !sameCookie(old, c) {
			changed[c.key()] = c
		}
	}

	var merged []Cookie
	seen := map[string]bool{}
	for _, c := range current {
		key := c.key()
		seen[key] = true
		_, wasInBase := inBase[key]
		switch next, ok := changed[key]; {
		case ok:
			merged = append(merged, next)
		case wasInBase && !inUpdated[key]:
			// deleted by the site during this download
		default:
			merged = append(merged, c)
		}
	}
	for _, c := range updated {
		if next, ok := changed[c.key()]; ok && !seen[c.key()] {
			merged = append(merged, next)
			seen[c.key()] = true
		}
	}
	return merged
}

// sameCookie compares everything but the HttpOnly flag, which yt-dlp
// drops when it writes its jar back.
func sameCookie(a, b Cookie) bool {
	a.HTTPOnly, b.HTTPOnly = false, false
	return a == b
}
