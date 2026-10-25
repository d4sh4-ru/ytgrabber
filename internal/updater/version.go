package updater

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a semantic version as release tags carry it: 1.2.3 or
// 1.2.3-rc.1 (a leading "v" is accepted).
type Version struct {
	Major, Minor, Patch int
	Pre                 []string // pre-release identifiers, nil for a release
}

func ParseVersion(raw string) (Version, error) {
	value := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	value, _, _ = strings.Cut(value, "+") // build metadata doesn't order
	core, pre, hasPre := strings.Cut(value, "-")

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("версия %q не похожа на X.Y.Z", raw)
	}
	var numbers [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || (len(part) > 1 && part[0] == '0') {
			return Version{}, fmt.Errorf("версия %q не похожа на X.Y.Z", raw)
		}
		numbers[i] = n
	}

	v := Version{Major: numbers[0], Minor: numbers[1], Patch: numbers[2]}
	if hasPre {
		if pre == "" {
			return Version{}, fmt.Errorf("версия %q не похожа на X.Y.Z", raw)
		}
		v.Pre = strings.Split(pre, ".")
	}
	return v, nil
}

func (v Version) IsPrerelease() bool {
	return len(v.Pre) > 0
}

func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.IsPrerelease() {
		s += "-" + strings.Join(v.Pre, ".")
	}
	return s
}

// Compare orders versions by semver precedence: -1, 0 or 1.
func (v Version) Compare(other Version) int {
	for _, pair := range [][2]int{{v.Major, other.Major}, {v.Minor, other.Minor}, {v.Patch, other.Patch}} {
		if c := compareInt(pair[0], pair[1]); c != 0 {
			return c
		}
	}
	// A pre-release comes before the release it leads to.
	switch {
	case !v.IsPrerelease() && !other.IsPrerelease():
		return 0
	case !v.IsPrerelease():
		return 1
	case !other.IsPrerelease():
		return -1
	}
	for i := 0; i < len(v.Pre) && i < len(other.Pre); i++ {
		if c := comparePre(v.Pre[i], other.Pre[i]); c != 0 {
			return c
		}
	}
	return compareInt(len(v.Pre), len(other.Pre))
}

// comparePre compares one pre-release identifier: numbers numerically and
// below words, words in ASCII order.
func comparePre(a, b string) int {
	na, errA := strconv.Atoi(a)
	nb, errB := strconv.Atoi(b)
	switch {
	case errA == nil && errB == nil:
		return compareInt(na, nb)
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
