package app

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstSentenceOf(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"short", "Hello world", "Hello world"},
		{"with_newline", "First line\nSecond line", "First line"},
		{"empty", "", ""},
		{"long_no_newline", strings.Repeat("a", 200), strings.Repeat("a", 150) + "..."},
		{"newline_after_150", strings.Repeat("a", 160) + "\nsecond", strings.Repeat("a", 150) + "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstSentenceOf(tt.input)
			if got != tt.expected {
				t.Errorf("firstSentenceOf = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFormatFieldType(t *testing.T) {
	tests := []struct {
		name     string
		expr     ast.Expr
		expected string
	}{
		{"ident", &ast.Ident{Name: "string"}, "string"},
		{"star", &ast.StarExpr{X: &ast.Ident{Name: "Config"}}, "*Config"},
		{"selector", &ast.SelectorExpr{X: &ast.Ident{Name: "domain"}, Sel: &ast.Ident{Name: "EpisodicRepo"}}, "domain.EpisodicRepo"},
		{"array", &ast.ArrayType{Elt: &ast.Ident{Name: "byte"}}, "[]byte"},
		{"map", &ast.MapType{Key: &ast.Ident{Name: "string"}, Value: &ast.Ident{Name: "int"}}, "map[string]int"},
		{"interface", &ast.InterfaceType{}, "interface{}"},
		{"func", &ast.FuncType{}, "func(...)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatFieldType(tt.expr)
			if got != tt.expected {
				t.Errorf("formatFieldType = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractGoChunks(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "sample.go")
	src := `package sample

// Greeter greets people.
type Greeter struct {
	Name string
}

// Greet returns a greeting message.
func (g *Greeter) Greet() string {
	return "Hello, " + g.Name
}

func PublicFunc(x int) int {
	return x * 2
}

func privateFunc() {}
`
	if err := os.WriteFile(goFile, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	chunks, err := extractGoChunks(goFile, "sample.go")
	if err != nil {
		t.Fatalf("extractGoChunks: %v", err)
	}

	// Should find: Greeter struct, Greet method, PublicFunc, privateFunc
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	var foundStruct, foundMethod, foundFunc bool
	for _, c := range chunks {
		switch {
		case c.Name == "Greeter" && c.Kind == "struct":
			foundStruct = true
			if !strings.Contains(c.Body, "Name string") {
				t.Error("struct body should contain field definitions")
			}
			if c.Package != "sample" {
				t.Errorf("package = %q, want sample", c.Package)
			}
		case c.Name == "*Greeter.Greet" && c.Kind == "method":
			foundMethod = true
			if !strings.Contains(c.Body, `"Hello, "`) {
				t.Error("method body should contain implementation")
			}
			if c.DocComment == "" {
				t.Error("method should have doc comment")
			}
		case c.Name == "PublicFunc" && c.Kind == "func":
			foundFunc = true
			if !strings.Contains(c.Body, "x * 2") {
				t.Error("func body should contain implementation")
			}
		}
	}

	if !foundStruct {
		t.Error("missing Greeter struct")
	}
	if !foundMethod {
		t.Error("missing Greet method")
	}
	if !foundFunc {
		t.Error("missing PublicFunc")
	}
}

func TestExtractGoChunks_NonExistent(t *testing.T) {
	_, err := extractGoChunks("/nonexistent/file.go", "file.go")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestExtractGenericChunks_Python(t *testing.T) {
	dir := t.TempDir()
	pyFile := filepath.Join(dir, "module.py")
	src := `# Utility functions
def hello(name):
    return f"Hello, {name}"

class Greeter:
    def __init__(self, name):
        self.name = name

    def greet(self):
        return f"Hi, {self.name}"
`
	if err := os.WriteFile(pyFile, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	chunks, err := extractGenericChunks(pyFile, "module.py", "python")
	if err != nil {
		t.Fatalf("extractGenericChunks: %v", err)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	var foundFunc, foundClass bool
	for _, c := range chunks {
		if c.Name == "hello" {
			foundFunc = true
			if c.Kind != "func" {
				t.Errorf("hello kind = %q, want func", c.Kind)
			}
		}
		if c.Name == "Greeter" {
			foundClass = true
			if c.Kind != "class" {
				t.Errorf("Greeter kind = %q, want class", c.Kind)
			}
		}
	}
	if !foundFunc {
		t.Error("missing hello function")
	}
	if !foundClass {
		t.Error("missing Greeter class")
	}
}

func TestExtractGenericChunks_TypeScript(t *testing.T) {
	dir := t.TempDir()
	tsFile := filepath.Join(dir, "app.ts")
	src := `export interface UserService {
  getUser(id: string): Promise<User>;
}

export class UserServiceImpl implements UserService {
  async getUser(id: string): Promise<User> {
    return await db.findUser(id);
  }
}

export function createApp(config: Config) {
  return new App(config);
}
`
	if err := os.WriteFile(tsFile, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	chunks, err := extractGenericChunks(tsFile, "app.ts", "typescript")
	if err != nil {
		t.Fatalf("extractGenericChunks: %v", err)
	}

	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}
}

func TestFormatChunkContent(t *testing.T) {
	c := codeChunk{
		Name:       "MyFunc",
		Kind:       "func",
		Lang:       "go",
		Package:    "app",
		FilePath:   "internal/app/service.go",
		Body:       "func MyFunc(x int) int {\n\treturn x * 2\n}",
		DocComment: "MyFunc doubles the input.",
	}
	got := formatChunkContent(c)
	if !strings.Contains(got, "[go func] MyFunc") {
		t.Errorf("expected header, got %q", got)
	}
	if !strings.Contains(got, "(package app)") {
		t.Error("expected package info")
	}
	if !strings.Contains(got, "return x * 2") {
		t.Error("expected function body")
	}
	if !strings.Contains(got, "MyFunc doubles") {
		t.Error("expected doc comment")
	}
}

func TestFormatChunkContent_Truncation(t *testing.T) {
	c := codeChunk{
		Name:     "Big",
		Kind:     "func",
		Lang:     "go",
		FilePath: "big.go",
		Body:     strings.Repeat("x", 3000),
	}
	got := formatChunkContent(c)
	if len(got) > 2200 {
		t.Errorf("expected truncation, got len=%d", len(got))
	}
	if !strings.Contains(got, "// ... truncated") {
		t.Error("expected truncation marker")
	}
}

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"service_test.go", true},
		{"service.go", false},
		{"app.test.ts", true},
		{"app.ts", false},
		{"test_module.py", true},
		{"module.py", false},
		{"UserTest.java", true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isTestFile(tt.path); got != tt.want {
				t.Errorf("isTestFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestDetectKind(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"class Foo {", "class"},
		{"export interface Bar {", "interface"},
		{"struct Point {", "struct"},
		{"def hello():", "func"},
		{"pub fn process()", "func"},
		{"  something else", "block"},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			if got := detectKind(tt.line, "go"); got != tt.want {
				t.Errorf("detectKind(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestChunkImportance(t *testing.T) {
	tests := []struct {
		kind     string
		bodyLen  int
		wantMin  float64
		wantMax  float64
	}{
		{"interface", 100, 0.70, 0.70},
		{"struct", 100, 0.60, 0.60},
		{"func", 100, 0.45, 0.45},
		{"func", 600, 0.55, 0.55},
		{"block", 100, 0.35, 0.35},
	}
	for _, tt := range tests {
		c := codeChunk{Kind: tt.kind, Body: strings.Repeat("x", tt.bodyLen)}
		got := chunkImportance(c)
		if got < tt.wantMin || got > tt.wantMax {
			t.Errorf("kind=%s bodyLen=%d: importance=%f, want [%f, %f]",
				tt.kind, tt.bodyLen, got, tt.wantMin, tt.wantMax)
		}
	}
}

func TestNameToWords(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"parseTemporalHint", "parse temporal hint"},
		{"ScoreAll", "score all"},
		{"score_all", "score all"},
		{"HTTPServer", "h t t p server"},
		{"simple", "simple"},
		{"A", "a"},
		{"getHTTPResponse", "get h t t p response"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := nameToWords(tt.input)
			if got != tt.want {
				t.Errorf("nameToWords(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestChunkEmbeddingText(t *testing.T) {
	c := codeChunk{
		Name:       "ProcessRequest",
		Kind:       "func",
		Lang:       "go",
		FilePath:   "internal/app/handler.go",
		Body:       "func ProcessRequest(r *Request) error { return nil }",
		DocComment: "ProcessRequest handles incoming API requests and validates input.",
	}
	got := chunkEmbeddingText(c)
	if !strings.Contains(got, "Function ProcessRequest") {
		t.Error("expected function prefix")
	}
	if !strings.Contains(got, "handles incoming API requests") {
		t.Error("expected doc comment in embedding text")
	}
	if !strings.Contains(got, "func ProcessRequest") {
		t.Error("expected code body")
	}
}

func TestChunkEmbeddingText_NoDoc(t *testing.T) {
	c := codeChunk{
		Name:     "parseTemporalHint",
		Kind:     "func",
		Lang:     "go",
		FilePath: "recall_service.go",
		Body:     "func parseTemporalHint() {}",
	}
	got := chunkEmbeddingText(c)
	// Without doc comment, should use nameToWords
	if !strings.Contains(got, "parse temporal hint") {
		t.Errorf("expected name-to-words fallback, got %q", got)
	}
}

func TestChunkTags(t *testing.T) {
	c := codeChunk{
		Kind:     "func",
		Lang:     "go",
		Package:  "app",
		FilePath: "internal/app/svc.go",
	}
	tags := chunkTags(c)
	want := []string{"code_chunk", "go", "func", "app", "internal/app/svc.go"}
	if len(tags) != len(want) {
		t.Fatalf("tags = %v, want %v", tags, want)
	}
	for i, w := range want {
		if tags[i] != w {
			t.Errorf("tags[%d] = %q, want %q", i, tags[i], w)
		}
	}
}
