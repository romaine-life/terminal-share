package server

import (
	"bytes"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxCols        = 1000
	maxRows        = 500
	maxTitleRunes  = 256
	maxFamilyRunes = 64
	paletteSize    = 16
)

var (
	hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	// A font family is drawn into CSS and canvas font strings, so it is one family name
	// with no quotes, commas or semicolons.
	fontFamily = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._+()-]*$`)
	idPattern  = regexp.MustCompile(`^[a-z2-7]{26}$`)
	idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)
)

// Theme is the snapshot terminal's colors: palette is ANSI 0-15 in order.
type Theme struct {
	Foreground string   `json:"foreground"`
	Background string   `json:"background"`
	Cursor     string   `json:"cursor"`
	Palette    []string `json:"palette"`
}

type Font struct {
	Family string `json:"family"`
}

// Snapshot is what a home PC posts: a cols x rows terminal and the VT stream that draws it.
type Snapshot struct {
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
	VT    string `json:"vt"`
	Title string `json:"title"`
	Theme Theme  `json:"theme"`
	Font  Font   `json:"font"`
}

// Record is a stored snapshot: GET /api/snapshots/<id> answers with it.
type Record struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
	Device    string `json:"device"`
	Snapshot
}

// The body as posted, with pointers so a missing field is told apart from a zero one.
type snapshotInput struct {
	Cols  *int    `json:"cols"`
	Rows  *int    `json:"rows"`
	VT    *string `json:"vt"`
	Title *string `json:"title"`
	Theme *struct {
		Foreground *string   `json:"foreground"`
		Background *string   `json:"background"`
		Cursor     *string   `json:"cursor"`
		Palette    *[]string `json:"palette"`
	} `json:"theme"`
	Font *struct {
		Family *string `json:"family"`
	} `json:"font"`
}

// parseSnapshot reads and checks a posted body. Its error is one sentence for the caller.
func parseSnapshot(body []byte) (Snapshot, error) {
	var in snapshotInput
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&in); err != nil {
		return Snapshot{}, decodeError(err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Snapshot{}, errors.New("The body must be a single JSON object.")
	}
	var s Snapshot
	switch {
	case in.Cols == nil:
		return s, errors.New("cols is required.")
	case *in.Cols < 1 || *in.Cols > maxCols:
		return s, fmt.Errorf("cols must be from 1 to %d.", maxCols)
	case in.Rows == nil:
		return s, errors.New("rows is required.")
	case *in.Rows < 1 || *in.Rows > maxRows:
		return s, fmt.Errorf("rows must be from 1 to %d.", maxRows)
	case in.VT == nil || *in.VT == "":
		return s, errors.New("vt is required: the VT stream that draws the screen.")
	case in.Title != nil && utf8.RuneCountInString(*in.Title) > maxTitleRunes:
		return s, fmt.Errorf("title must be at most %d characters.", maxTitleRunes)
	case in.Theme == nil:
		return s, errors.New("theme is required.")
	case in.Font == nil || in.Font.Family == nil || strings.TrimSpace(*in.Font.Family) == "":
		return s, errors.New("font.family is required.")
	}
	family := strings.TrimSpace(*in.Font.Family)
	if utf8.RuneCountInString(family) > maxFamilyRunes || !fontFamily.MatchString(family) {
		return s, fmt.Errorf("font.family must be one font family name of at most %d letters, digits, spaces and . _ + ( ) -.", maxFamilyRunes)
	}
	for _, c := range []struct {
		name  string
		value *string
	}{{"theme.foreground", in.Theme.Foreground}, {"theme.background", in.Theme.Background}, {"theme.cursor", in.Theme.Cursor}} {
		if c.value == nil || !hexColor.MatchString(*c.value) {
			return s, fmt.Errorf("%s must be a #RRGGBB color.", c.name)
		}
	}
	if in.Theme.Palette == nil || len(*in.Theme.Palette) != paletteSize {
		return s, fmt.Errorf("theme.palette must hold exactly %d colors, ANSI 0 to 15.", paletteSize)
	}
	for i, color := range *in.Theme.Palette {
		if !hexColor.MatchString(color) {
			return s, fmt.Errorf("theme.palette[%d] must be a #RRGGBB color.", i)
		}
	}
	s = Snapshot{
		Cols: *in.Cols,
		Rows: *in.Rows,
		VT:   *in.VT,
		Theme: Theme{
			Foreground: *in.Theme.Foreground,
			Background: *in.Theme.Background,
			Cursor:     *in.Theme.Cursor,
			Palette:    *in.Theme.Palette,
		},
		Font: Font{Family: family},
	}
	if in.Title != nil {
		s.Title = *in.Title
	}
	return s, nil
}

// What each field must be, for the sentence a wrongly typed one gets.
var fieldKinds = map[string]string{
	"cols":             "a whole number",
	"rows":             "a whole number",
	"vt":               "a string",
	"title":            "a string",
	"theme":            "an object",
	"theme.foreground": "a #RRGGBB color",
	"theme.background": "a #RRGGBB color",
	"theme.cursor":     "a #RRGGBB color",
	"theme.palette":    "a list of 16 #RRGGBB colors",
	"font":             "an object",
	"font.family":      "a string",
}

func decodeError(err error) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		if typeErr.Field == "" {
			return errors.New("The body must be a JSON object.")
		}
		if kind, ok := fieldKinds[typeErr.Field]; ok {
			return fmt.Errorf("%s must be %s.", typeErr.Field, kind)
		}
		return fmt.Errorf("%s has the wrong type.", typeErr.Field)
	}
	return errors.New("The body is not valid JSON.")
}

// newID is 128 random bits as lowercase base32 without padding: 26 characters.
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails; it aborts the program instead.
	return strings.ToLower(idEncoding.EncodeToString(b[:]))
}

func validID(id string) bool { return idPattern.MatchString(id) }
