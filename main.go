package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

//go:embed catalog/books.json fixtures/readers.json
var content embed.FS

type Passage struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}
type Book struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Author   string    `json:"author"`
	Note     string    `json:"note"`
	Passages []Passage `json:"passages"`
}
type Position struct {
	Index  int `json:"index"`
	Offset int `json:"offset,omitempty"` // word offset within the paragraph/stanza
}
type Bookmark struct {
	Position
	Undo []Position `json:"undo,omitempty"`
}
type Reader struct {
	User      string               `json:"user"`
	Current   string               `json:"current"`
	Bookmarks map[string]*Bookmark `json:"bookmarks"`
}
type Library struct {
	Books []Book
	byID  map[string]*Book
	Dir   string
	mu    sync.Mutex // serializes changes made by this running process
}

var validUser = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func loadLibrary(dir string) (*Library, error) {
	bytes, err := content.ReadFile("catalog/books.json")
	if err != nil {
		return nil, err
	}
	var books []Book
	if err = json.Unmarshal(bytes, &books); err != nil {
		return nil, err
	}
	lib := &Library{Books: books, byID: make(map[string]*Book), Dir: dir}
	for i := range books {
		b := &lib.Books[i]
		if b.ID == "" || len(b.Passages) == 0 || lib.byID[b.ID] != nil {
			return nil, fmt.Errorf("bad book: %q", b.ID)
		}
		lib.byID[b.ID] = b
		seen := map[string]bool{}
		for _, p := range b.Passages {
			if p.ID == "" || seen[p.ID] || p.Text == "" {
				return nil, fmt.Errorf("bad passage in %s", b.ID)
			}
			seen[p.ID] = true
		}
	}
	return lib, nil
}
func (l *Library) statePath(id string) (string, error) {
	if !validUser.MatchString(id) {
		return "", errors.New("invalid mock reader ID")
	}
	return filepath.Join(l.Dir, id+".json"), nil
}
func (l *Library) read(id string) (*Reader, error) {
	path, err := l.statePath(id)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Reader{User: id, Bookmarks: make(map[string]*Bookmark)}, nil
	}
	if err != nil {
		return nil, err
	}
	var r Reader
	if err = json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if r.User != id {
		return nil, errors.New("reader ID mismatch")
	}
	if r.Bookmarks == nil {
		r.Bookmarks = make(map[string]*Bookmark)
	}
	return &r, nil
}
func (l *Library) write(r *Reader) error {
	path, err := l.statePath(r.User)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(l.Dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(l.Dir, ".reader-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err = enc.Encode(r); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (l *Library) books() string {
	var s strings.Builder
	s.WriteString("BOOKS\n")
	for i, b := range l.Books {
		fmt.Fprintf(&s, "%d %s — %s\n", i+1, b.Title, b.Author)
	}
	s.WriteString("Reply SELECT <number or ID>.")
	return s.String()
}
func (l *Library) findBook(input string) *Book {
	if b := l.byID[strings.ToLower(input)]; b != nil {
		return b
	}
	n, err := strconv.Atoi(input)
	if err == nil && n >= 1 && n <= len(l.Books) {
		return &l.Books[n-1]
	}
	return nil
}

// remainder keeps the original newlines (particularly important for poetry).
func remainder(text string, wordsToSkip int) string {
	if wordsToSkip <= 0 {
		return text
	}
	inWord := false
	word := 0
	for at, r := range text {
		if unicode.IsSpace(r) {
			inWord = false
			continue
		}
		if !inWord {
			word++
			if word > wordsToSkip {
				return text[at:]
			}
			inWord = true
		}
	}
	return ""
}
func display(b *Book, p Position) string {
	if p.Index < 0 {
		return b.Title + " selected. Reply CONTINUE."
	}
	if p.Index >= len(b.Passages) {
		return "END OF MOCK EXCERPT — " + b.Title
	}
	passage := b.Passages[p.Index]
	if !utf8.ValidString(passage.Text) {
		return "Invalid UTF-8 in mock passage"
	}
	return fmt.Sprintf("%s / %s (%s)\n%s", b.Title, passage.ID, passage.Kind, remainder(passage.Text, p.Offset))
}
func remember(m *Bookmark) {
	m.Undo = append(m.Undo, m.Position)
	if len(m.Undo) > 20 {
		m.Undo = m.Undo[len(m.Undo)-20:]
	}
}
func (l *Library) jog(id, command string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, err := l.read(id)
	if err != nil {
		return "", err
	}
	input := strings.ToLower(strings.Join(strings.Fields(command), " "))
	switch input {
	case "books", "library", "list":
		return l.books(), nil
	case "help", "?":
		return "BOOKS, SELECT <number>, CONTINUE/N/C/GO/NEXT/YES, BACK/B, HALF BACK/HB, RESTART, UNDO, AGAIN", nil
	}
	if strings.HasPrefix(input, "select ") || strings.HasPrefix(input, "read ") {
		idOrNumber := strings.TrimSpace(input[strings.Index(input, " ")+1:])
		b := l.findBook(idOrNumber)
		if b == nil {
			return "", errors.New("unknown book (use BOOKS)")
		}
		r.Current = b.ID
		if r.Bookmarks[b.ID] == nil {
			r.Bookmarks[b.ID] = &Bookmark{Position: Position{Index: -1}}
		}
		if err = l.write(r); err != nil {
			return "", err
		}
		return display(b, r.Bookmarks[b.ID].Position), nil
	}
	if r.Current == "" {
		return "", errors.New("no selected book; send BOOKS and SELECT <number>")
	}
	b := l.byID[r.Current]
	if b == nil {
		return "", errors.New("selected book absent from catalog")
	}
	m := r.Bookmarks[b.ID]
	if m == nil {
		m = &Bookmark{Position: Position{Index: -1}}
		r.Bookmarks[b.ID] = m
	}
	switch input {
	case "continue", "c", "n", "next", "go", "yes":
		if m.Index >= len(b.Passages)-1 {
			return "END OF MOCK EXCERPT — " + b.Title, nil
		}
		remember(m)
		m.Index++
		m.Offset = 0
	case "back", "b":
		if m.Index <= 0 {
			return display(b, m.Position), nil
		}
		remember(m)
		m.Index--
		m.Offset = 0
	case "half back", "halfback", "hb":
		if m.Index < 0 {
			return display(b, m.Position), nil
		}
		remember(m)
		if m.Index > 0 {
			m.Index--
		}
		m.Offset = len(strings.Fields(b.Passages[m.Index].Text)) / 2
	case "restart", "start over":
		remember(m)
		m.Index = 0
		m.Offset = 0
	case "undo", "u":
		if len(m.Undo) == 0 {
			return "Nothing to undo.", nil
		}
		m.Position = m.Undo[len(m.Undo)-1]
		m.Undo = m.Undo[:len(m.Undo)-1]
	case "again", "repeat", "a":
		return display(b, m.Position), nil
	default:
		return "", errors.New("unknown command; send HELP")
	}
	if err = l.write(r); err != nil {
		return "", err
	}
	return display(b, m.Position), nil
}
func (l *Library) seed() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	data, err := content.ReadFile("fixtures/readers.json")
	if err != nil {
		return err
	}
	var readers []Reader
	if err = json.Unmarshal(data, &readers); err != nil {
		return err
	}
	for i := range readers {
		path, err := l.statePath(readers[i].User)
		if err != nil {
			return err
		}
		if _, err = os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err = l.write(&readers[i]); err != nil {
			return err
		}
	}
	return nil
}
func (l *Library) serve(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("GET /books", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, l.books())
	})
	mux.HandleFunc("GET /state/{user}", func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		reader, err := l.read(r.PathValue("user"))
		l.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(reader)
	})
	mux.HandleFunc("POST /jog/{user}", func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 256 {
			http.Error(w, "command too long", 413)
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 257))
		if err != nil || len(data) > 256 {
			http.Error(w, "command too long", 413)
			return
		}
		response, err := l.jog(r.PathValue("user"), string(data))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, response)
	})
	log.Printf("mock jog listening on %s (no authentication; bind to loopback)", addr)
	return http.ListenAndServe(addr, mux)
}
func usage() {
	fmt.Fprintln(os.Stderr, "usage: sms-book-reader seed | books | jog USER COMMAND | state USER | serve [127.0.0.1:8787]")
}
func main() {
	dir := os.Getenv("SMS_READER_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		dir = filepath.Join(home, ".local", "state", "sms-book-reader")
	}
	l, err := loadLibrary(dir)
	if err != nil {
		log.Fatal(err)
	}
	args := os.Args[1:]
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}
	var output string
	switch args[0] {
	case "seed":
		err = l.seed()
		output = "Mock users: ada, ben, cy"
	case "books":
		output = l.books()
	case "jog":
		if len(args) < 3 {
			usage()
			os.Exit(2)
		}
		output, err = l.jog(args[1], strings.Join(args[2:], " "))
	case "state":
		if len(args) < 2 {
			usage()
			os.Exit(2)
		}
		l.mu.Lock()
		var r *Reader
		r, err = l.read(args[1])
		l.mu.Unlock()
		if err == nil {
			var raw []byte
			raw, err = json.MarshalIndent(r, "", "  ")
			output = string(raw)
		}
	case "serve":
		addr := "127.0.0.1:8787"
		if len(args) > 1 {
			addr = args[1]
		}
		err = l.serve(addr)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
	if output != "" {
		fmt.Println(output)
	}
}
