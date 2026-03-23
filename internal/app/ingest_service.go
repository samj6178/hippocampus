package app

import (
	"bufio"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

type IngestService struct {
	semantic  domain.SemanticRepo
	embedding domain.EmbeddingProvider
	logger    *slog.Logger
}

func NewIngestService(
	semantic domain.SemanticRepo,
	embedding domain.EmbeddingProvider,
	logger *slog.Logger,
) *IngestService {
	return &IngestService{
		semantic:  semantic,
		embedding: embedding,
		logger:    logger,
	}
}

type IngestResult struct {
	FilesScanned    int      `json:"files_scanned"`
	EntitiesFound   int      `json:"entities_found"`
	MemoriesCreated int      `json:"memories_created"`
	Updated         int      `json:"updated"`
	Skipped         int      `json:"skipped"`
	Duplicates      int      `json:"duplicates"`
	Errors          []string `json:"errors,omitempty"`
}

// codeChunk represents a function, method, struct, or other code block
// extracted from a source file with its full body.
type codeChunk struct {
	Name     string
	Kind     string // "func", "method", "struct", "interface", "class", "block"
	Lang     string // "go", "typescript", "python", "rust", etc.
	Package  string
	FilePath string
	Body     string // actual source code
	DocComment string
}

// ingestSkipDirs are directories to skip during codebase ingestion.
var ingestSkipDirs = map[string]bool{
	"vendor": true, "node_modules": true, ".git": true,
	"web_dist": true, "dist": true, "build": true,
	"__pycache__": true, ".next": true, "target": true,
	".venv": true, "venv": true,
}

var langExtensions = map[string]string{
	".go":   "go",
	".ts":   "typescript",
	".tsx":  "typescript",
	".js":   "javascript",
	".jsx":  "javascript",
	".py":   "python",
	".rs":   "rust",
	".cpp":  "cpp",
	".cc":   "cpp",
	".c":    "c",
	".h":    "c",
	".hpp":  "cpp",
	".java": "java",
	".rb":   "ruby",
	".cs":   "csharp",
}

var testPatterns = []string{"_test.go", ".test.ts", ".test.tsx", ".spec.ts", ".spec.tsx",
	"_test.py", "test_", "_test.rs", "Test.java"}

// IngestProject walks a directory tree, extracts code chunks from source files,
// and stores them as semantic memories. Supports Go (AST), and generic regex-based
// chunking for TypeScript, Python, Rust, and other languages.
func (s *IngestService) IngestProject(ctx context.Context, rootPath string, projectID *uuid.UUID) (*IngestResult, error) {
	if rootPath == "" {
		return nil, fmt.Errorf("root_path is required")
	}
	info, err := os.Stat(rootPath)
	if err != nil {
		return nil, fmt.Errorf("invalid path %q: %w", rootPath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", rootPath)
	}

	result := &IngestResult{}
	var allChunks []codeChunk

	err = filepath.Walk(rootPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			if ingestSkipDirs[filepath.Base(path)] {
				return filepath.SkipDir
			}
			return nil
		}
		if fi.Size() > 512*1024 { // skip files > 512KB
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		lang, ok := langExtensions[ext]
		if !ok {
			return nil
		}
		if isTestFile(path) {
			return nil
		}

		result.FilesScanned++
		relPath, _ := filepath.Rel(rootPath, path)
		relPath = filepath.ToSlash(relPath)

		var chunks []codeChunk
		var parseErr error

		if lang == "go" {
			chunks, parseErr = extractGoChunks(path, relPath)
		} else {
			chunks, parseErr = extractGenericChunks(path, relPath, lang)
		}

		if parseErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", relPath, parseErr))
			return nil
		}

		allChunks = append(allChunks, chunks...)
		result.EntitiesFound += len(chunks)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %q: %w", rootPath, err)
	}

	// Store each chunk as a semantic memory with dedup
	for _, chunk := range allChunks {
		if ctx.Err() != nil {
			break
		}

		content := formatChunkContent(chunk)
		if len(content) < 30 {
			result.Skipped++
			continue
		}

		// Embed a NL-enriched version for better semantic matching.
		// The stored content keeps full code, but the embedding is computed
		// from a description that nomic-embed-text matches better.
		embText := chunkEmbeddingText(chunk)
		emb, err := s.embedding.Embed(ctx, embText)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("embed %s/%s: %v", chunk.FilePath, chunk.Name, err))
			continue
		}

		// Incremental: if similar memory exists, update it instead of creating new.
		// This handles code changes — re-ingest updates the memory, not duplicates it.
		existing := s.findExisting(ctx, emb, projectID)
		if existing != nil {
			// Update only if content actually changed
			if existing.Content == content {
				result.Duplicates++
				continue
			}
			existing.Content = content
			existing.Embedding = emb
			existing.TokenCount = estimateTokens(content)
			existing.UpdatedAt = time.Now()
			existing.Tags = chunkTags(chunk)
			if err := s.semantic.Update(ctx, existing); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("update %s/%s: %v", chunk.FilePath, chunk.Name, err))
			} else {
				result.Updated++
			}
			continue
		}

		importance := chunkImportance(chunk)
		now := time.Now()
		sem := &domain.SemanticMemory{
			MemoryItem: domain.MemoryItem{
				ID:           uuid.New(),
				ProjectID:    projectID,
				Tier:         domain.TierSemantic,
				Content:      content,
				Embedding:    emb,
				Importance:   importance,
				Confidence:   0.95,
				TokenCount:   estimateTokens(content),
				LastAccessed: now,
				CreatedAt:    now,
				UpdatedAt:    now,
				Tags:         chunkTags(chunk),
			},
			EntityType: "code_chunk",
		}

		if err := s.semantic.Insert(ctx, sem); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("insert %s/%s: %v", chunk.FilePath, chunk.Name, err))
			continue
		}
		result.MemoriesCreated++
	}

	s.logger.Info("codebase ingestion completed",
		"files", result.FilesScanned,
		"entities", result.EntitiesFound,
		"created", result.MemoriesCreated,
		"duplicates", result.Duplicates,
		"skipped", result.Skipped,
	)
	return result, nil
}

// IngestGoProject is kept for backward compatibility.
func (s *IngestService) IngestGoProject(ctx context.Context, rootPath string, projectID *uuid.UUID) (*IngestResult, error) {
	return s.IngestProject(ctx, rootPath, projectID)
}

// findExisting checks if a similar code chunk memory already exists.
// Returns the existing memory ID for update, or nil for insert.
// Threshold 0.92 = near-identical content (same function, minor changes).
func (s *IngestService) findExisting(ctx context.Context, emb []float32, projectID *uuid.UUID) *domain.SemanticMemory {
	similar, err := s.semantic.SearchSimilar(ctx, emb, projectID, 1)
	if err != nil || len(similar) == 0 {
		return nil
	}
	if similar[0].Similarity >= 0.92 {
		return similar[0]
	}
	return nil
}

func chunkImportance(c codeChunk) float64 {
	switch c.Kind {
	case "interface":
		return 0.70
	case "struct", "class":
		return 0.60
	case "func", "method":
		if len(c.Body) > 500 {
			return 0.55 // complex functions are more important
		}
		return 0.45
	default:
		return 0.35
	}
}

func chunkTags(c codeChunk) []string {
	tags := []string{"code_chunk", c.Lang, c.Kind}
	if c.Package != "" {
		tags = append(tags, c.Package)
	}
	if c.FilePath != "" {
		tags = append(tags, c.FilePath)
	}
	return tags
}

func formatChunkContent(c codeChunk) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[%s %s] %s in %s", c.Lang, c.Kind, c.Name, c.FilePath))
	if c.Package != "" {
		b.WriteString(fmt.Sprintf(" (package %s)", c.Package))
	}
	b.WriteString("\n")
	if c.DocComment != "" {
		b.WriteString(c.DocComment)
		b.WriteString("\n")
	}
	body := c.Body
	const maxBody = 2000 // ~500 tokens
	if len(body) > maxBody {
		body = body[:maxBody] + "\n// ... truncated"
	}
	b.WriteString(body)
	return b.String()
}

// chunkEmbeddingText generates a natural-language enriched text for embedding.
// Code tokens match poorly against NL queries in nomic-embed-text.
// This adds a descriptive prefix so "how does scoring work" matches
// a function that computes scores.
func chunkEmbeddingText(c codeChunk) string {
	var b strings.Builder

	// NL description line
	switch c.Kind {
	case "func", "method":
		b.WriteString(fmt.Sprintf("Function %s in %s: ", c.Name, c.FilePath))
	case "struct", "class":
		b.WriteString(fmt.Sprintf("Data structure %s in %s: ", c.Name, c.FilePath))
	case "interface":
		b.WriteString(fmt.Sprintf("Interface %s in %s defines the contract for: ", c.Name, c.FilePath))
	default:
		b.WriteString(fmt.Sprintf("Code block %s in %s: ", c.Name, c.FilePath))
	}

	// Add doc comment as the primary semantic signal
	if c.DocComment != "" {
		b.WriteString(c.DocComment)
	} else {
		// Extract semantic hints from the name (camelCase/snake_case → words)
		b.WriteString(nameToWords(c.Name))
	}
	b.WriteString(". ")

	// Add a trimmed version of the body for keyword matching
	body := c.Body
	if len(body) > 600 {
		body = body[:600]
	}
	b.WriteString(body)

	return b.String()
}

// nameToWords splits camelCase or snake_case into space-separated words.
// "parseTemporalHint" → "parse temporal hint"
// "score_all" → "score all"
func nameToWords(name string) string {
	var words []string
	var current strings.Builder
	for i, r := range name {
		if r == '_' || r == '-' {
			if current.Len() > 0 {
				words = append(words, strings.ToLower(current.String()))
				current.Reset()
			}
			continue
		}
		if i > 0 && r >= 'A' && r <= 'Z' {
			if current.Len() > 0 {
				words = append(words, strings.ToLower(current.String()))
				current.Reset()
			}
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		words = append(words, strings.ToLower(current.String()))
	}
	return strings.Join(words, " ")
}

func isTestFile(path string) bool {
	base := filepath.Base(path)
	for _, pat := range testPatterns {
		if strings.Contains(base, pat) {
			return true
		}
	}
	return false
}

// --- Go AST extraction with function bodies ---

func extractGoChunks(filePath, relPath string) ([]codeChunk, error) {
	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	pkgName := f.Name.Name
	var chunks []codeChunk

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				kind := "type"
				switch ts.Type.(type) {
				case *ast.StructType:
					kind = "struct"
				case *ast.InterfaceType:
					kind = "interface"
				}

				doc := ""
				if d.Doc != nil {
					doc = d.Doc.Text()
				}

				body := extractSourceRange(src, fset, d.Pos(), d.End())
				chunks = append(chunks, codeChunk{
					Name:       ts.Name.Name,
					Kind:       kind,
					Lang:       "go",
					Package:    pkgName,
					FilePath:   relPath,
					Body:       body,
					DocComment: strings.TrimSpace(doc),
				})
			}

		case *ast.FuncDecl:
			kind := "func"
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				kind = "method"
				recv := formatFieldType(d.Recv.List[0].Type)
				name = recv + "." + d.Name.Name
			}

			doc := ""
			if d.Doc != nil {
				doc = d.Doc.Text()
			}

			body := extractSourceRange(src, fset, d.Pos(), d.End())
			chunks = append(chunks, codeChunk{
				Name:       name,
				Kind:       kind,
				Lang:       "go",
				Package:    pkgName,
				FilePath:   relPath,
				Body:       body,
				DocComment: strings.TrimSpace(doc),
			})
		}
	}

	return chunks, nil
}

func extractSourceRange(src []byte, fset *token.FileSet, start, end token.Pos) string {
	startOff := fset.Position(start).Offset
	endOff := fset.Position(end).Offset
	if startOff < 0 || endOff > len(src) || startOff >= endOff {
		return ""
	}
	return string(src[startOff:endOff])
}

// --- Generic regex-based extraction for non-Go languages ---

// funcPatterns matches function/class/method definitions per language.
var funcPatterns = map[string]*regexp.Regexp{
	"typescript": regexp.MustCompile(`(?m)^(?:export\s+)?(?:async\s+)?(?:function|const|class|interface|type|enum)\s+(\w+)`),
	"javascript": regexp.MustCompile(`(?m)^(?:export\s+)?(?:async\s+)?(?:function|const|class)\s+(\w+)`),
	"python":     regexp.MustCompile(`(?m)^(?:async\s+)?(?:def|class)\s+(\w+)`),
	"rust":       regexp.MustCompile(`(?m)^(?:pub\s+)?(?:async\s+)?(?:fn|struct|enum|trait|impl|type)\s+(\w+)`),
	"cpp":        regexp.MustCompile(`(?m)^(?:\w+\s+)*(?:class|struct|namespace)\s+(\w+)|^(?:\w+(?:::\w+)*\s+)+(\w+)\s*\(`),
	"c":          regexp.MustCompile(`(?m)^(?:\w+\s+)+(\w+)\s*\(`),
	"java":       regexp.MustCompile(`(?m)^(?:public|private|protected)?\s*(?:static\s+)?(?:class|interface|enum|(?:\w+\s+)+)(\w+)\s*[({]`),
	"ruby":       regexp.MustCompile(`(?m)^(?:def|class|module)\s+(\w+)`),
	"csharp":     regexp.MustCompile(`(?m)^(?:public|private|protected|internal)?\s*(?:static\s+)?(?:partial\s+)?(?:class|struct|interface|enum|(?:\w+\s+)+)(\w+)\s*[({]`),
}

func extractGenericChunks(filePath, relPath, lang string) ([]codeChunk, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	pattern, ok := funcPatterns[lang]
	if !ok {
		// Fallback: store entire file as one chunk if small enough
		content := strings.Join(lines, "\n")
		if len(content) > 3000 {
			return nil, nil // too big, no pattern
		}
		return []codeChunk{{
			Name:     filepath.Base(relPath),
			Kind:     "block",
			Lang:     lang,
			FilePath: relPath,
			Body:     content,
		}}, nil
	}

	return splitByPattern(lines, pattern, relPath, lang), nil
}

// splitByPattern splits source lines into chunks at each line matching pattern.
// Each chunk runs from one match to the next (or end of file).
func splitByPattern(lines []string, pattern *regexp.Regexp, relPath, lang string) []codeChunk {
	type boundary struct {
		lineIdx int
		name    string
		kind    string
	}

	var boundaries []boundary
	for i, line := range lines {
		matches := pattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		name := ""
		for _, m := range matches[1:] {
			if m != "" {
				name = m
				break
			}
		}
		if name == "" {
			continue
		}

		kind := detectKind(line, lang)
		boundaries = append(boundaries, boundary{lineIdx: i, name: name, kind: kind})
	}

	if len(boundaries) == 0 {
		return nil
	}

	var chunks []codeChunk
	for idx, bd := range boundaries {
		endLine := len(lines)
		if idx+1 < len(boundaries) {
			endLine = boundaries[idx+1].lineIdx
		}

		// Include up to 2 lines before the match (doc comments)
		startLine := bd.lineIdx
		if startLine >= 2 && isComment(lines[startLine-1], lang) {
			startLine--
			if startLine >= 1 && isComment(lines[startLine-1], lang) {
				startLine--
			}
		}

		body := strings.Join(lines[startLine:endLine], "\n")
		body = strings.TrimRight(body, "\n\r\t ")

		if len(body) < 20 {
			continue
		}

		chunks = append(chunks, codeChunk{
			Name:     bd.name,
			Kind:     bd.kind,
			Lang:     lang,
			FilePath: relPath,
			Body:     body,
		})
	}

	return chunks
}

func detectKind(line, lang string) string {
	lower := strings.ToLower(strings.TrimSpace(line))
	switch {
	case strings.Contains(lower, "class "):
		return "class"
	case strings.Contains(lower, "interface "):
		return "interface"
	case strings.Contains(lower, "struct "):
		return "struct"
	case strings.Contains(lower, "trait "):
		return "interface"
	case strings.Contains(lower, "enum "):
		return "struct"
	case strings.Contains(lower, "impl "):
		return "method"
	case strings.Contains(lower, "def ") || strings.Contains(lower, "fn ") ||
		strings.Contains(lower, "function ") || strings.Contains(lower, "func "):
		return "func"
	case strings.Contains(lower, "const ") || strings.Contains(lower, "type "):
		return "func" // treat const/type as func-level
	default:
		return "block"
	}
}

func isComment(line, lang string) bool {
	trimmed := strings.TrimSpace(line)
	switch lang {
	case "python":
		return strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "\"\"\"")
	case "ruby":
		return strings.HasPrefix(trimmed, "#")
	default:
		return strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") ||
			strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/**")
	}
}

// --- helpers kept from original for backward compatibility ---

func formatFieldType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + formatFieldType(t.X)
	case *ast.SelectorExpr:
		return formatFieldType(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + formatFieldType(t.Elt)
	case *ast.MapType:
		return "map[" + formatFieldType(t.Key) + "]" + formatFieldType(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func(...)"
	default:
		return "..."
	}
}

func firstSentenceOf(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "\n"); idx > 0 && idx < 150 {
		return s[:idx]
	}
	if len(s) > 150 {
		return s[:150] + "..."
	}
	return s
}
