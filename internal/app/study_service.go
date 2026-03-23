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
)

type StudyService struct {
	encode *EncodeService
	logger *slog.Logger
}

func NewStudyService(encode *EncodeService, logger *slog.Logger) *StudyService {
	return &StudyService{encode: encode, logger: logger}
}

type StudyResult struct {
	FilesRead       int            `json:"files_read"`
	CodeFiles       int            `json:"code_files"`
	DocFiles        int            `json:"doc_files"`
	MemoriesCreated int            `json:"memories_created"`
	Skipped         int            `json:"skipped"`
	Files           []string       `json:"files_studied"`
	ByLanguage      map[string]int `json:"by_language"`
	Duration        time.Duration  `json:"duration"`
	Warnings        []string       `json:"warnings,omitempty"`
}

// Directories to always skip.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "__pycache__": true,
	"dist": true, "build": true, ".next": true, ".nuxt": true, "coverage": true,
	".idea": true, ".vscode": true, "bin": true, "obj": true, "target": true,
	".cache": true, ".turbo": true, ".parcel-cache": true, ".svelte-kit": true,
}

// Binary/media extensions to skip.
var skipExtensions = map[string]bool{
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".o": true, ".a": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".ico": true, ".webp": true,
	".mp3": true, ".mp4": true, ".wav": true, ".avi": true, ".mov": true,
	".zip": true, ".tar": true, ".gz": true, ".rar": true, ".7z": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".db": true, ".sqlite": true, ".lock": true, ".sum": true,
	".map": true, ".min.js": true, ".min.css": true,
	".pb.go": true, ".gen.go": true, ".generated.go": true,
}

// Code file extensions and their language names.
var codeExtensions = map[string]string{
	".go":    "go",
	".ts":    "typescript",
	".tsx":   "typescript-react",
	".js":    "javascript",
	".jsx":   "javascript-react",
	".py":    "python",
	".rs":    "rust",
	".java":  "java",
	".kt":    "kotlin",
	".cs":    "csharp",
	".rb":    "ruby",
	".php":   "php",
	".swift": "swift",
	".c":     "c",
	".cpp":   "cpp",
	".h":     "c-header",
	".hpp":   "cpp-header",
	".lua":   "lua",
	".sh":    "shell",
	".bash":  "shell",
	".ps1":   "powershell",
	".sql":   "sql",
	".proto": "protobuf",
	".graphql": "graphql",
	".gql":    "graphql",
}

// Config/doc extensions with lower importance.
var docExtensions = map[string]string{
	".md":    "markdown",
	".mdc":   "cursor-rule",
	".yml":   "yaml",
	".yaml":  "yaml",
	".toml":  "toml",
	".json":  "json",
	".env":   "env",
	".cfg":   "config",
	".ini":   "config",
	".conf":  "config",
}

var secretPatterns = []string{
	"password", "passwd", "secret", "api_key", "apikey",
	"token", "private_key", "credentials", "auth_token",
	"access_key", "secret_key",
}

func (s *StudyService) Study(ctx context.Context, rootPath string, projectID *uuid.UUID) (*StudyResult, error) {
	if rootPath == "" {
		return nil, fmt.Errorf("root_path is required")
	}
	start := time.Now()

	result := &StudyResult{
		ByLanguage: make(map[string]int),
	}
	sessionID := uuid.New()
	seen := make(map[string]bool)

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if info.IsDir() {
			base := filepath.Base(path)
			if skipDirs[base] || strings.HasPrefix(base, ".") && base != "." && base != ".cursor" && base != ".claude" {
				return filepath.SkipDir
			}
			return nil
		}

		if info.Size() > 512*1024 {
			result.Skipped++
			return nil
		}
		if info.Size() < 10 {
			result.Skipped++
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		base := strings.ToLower(filepath.Base(path))

		if skipExtensions[ext] {
			return nil
		}
		if strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".min.css") {
			return nil
		}

		relPath, _ := filepath.Rel(rootPath, path)
		relPath = filepath.ToSlash(relPath)

		if seen[relPath] {
			return nil
		}
		seen[relPath] = true

		if lang, ok := codeExtensions[ext]; ok {
			s.studyCodeFile(ctx, path, relPath, lang, projectID, sessionID, result)
		} else if lang, ok := docExtensions[ext]; ok {
			s.studyDocFile(ctx, path, relPath, lang, projectID, sessionID, result)
		} else if isSpecialFile(base) {
			s.studyDocFile(ctx, path, relPath, "config", projectID, sessionID, result)
		}

		return nil
	})

	if err != nil && err != ctx.Err() {
		result.Warnings = append(result.Warnings, fmt.Sprintf("walk error: %v", err))
	}

	result.Duration = time.Since(start)

	s.logger.Info("project study completed",
		"root", rootPath,
		"total_files", result.FilesRead,
		"code_files", result.CodeFiles,
		"doc_files", result.DocFiles,
		"memories", result.MemoriesCreated,
		"skipped", result.Skipped,
		"duration", result.Duration,
	)

	return result, nil
}

// studyCodeFile reads a code file and creates a structured profile memory.
func (s *StudyService) studyCodeFile(ctx context.Context, fullPath, relPath, lang string, projectID *uuid.UUID, sessionID uuid.UUID, result *StudyResult) {
	var profile string
	var err error

	if lang == "go" {
		profile, err = s.profileGoFile(fullPath, relPath)
	} else {
		profile, err = s.profileGenericFile(fullPath, relPath, lang)
	}

	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("profile %s: %v", relPath, err))
		return
	}
	if len(profile) < 20 {
		result.Skipped++
		return
	}

	if containsSecrets(profile) {
		profile = redactSecrets(profile)
		result.Warnings = append(result.Warnings, fmt.Sprintf("REDACTED secrets in %s", relPath))
	}

	importance := codeFileImportance(relPath)
	tags := codeFileTags(relPath, lang)

	_, err = s.encode.Encode(ctx, &EncodeRequest{
		Content:    profile,
		ProjectID:  projectID,
		AgentID:    "hippocampus-study",
		SessionID:  sessionID,
		Importance: importance,
		Tags:       tags,
	})
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("encode %s: %v", relPath, err))
		return
	}

	result.FilesRead++
	result.CodeFiles++
	result.MemoriesCreated++
	result.Files = append(result.Files, relPath)
	result.ByLanguage[lang]++
}

// profileGoFile uses go/ast to extract a rich structural summary.
func (s *StudyService) profileGoFile(fullPath, relPath string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, fullPath, nil, parser.ParseComments)
	if err != nil {
		return s.profileGenericFile(fullPath, relPath, "go")
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("CODE FILE: %s [go]\n", relPath))
	b.WriteString(fmt.Sprintf("package %s\n\n", f.Name.Name))

	if len(f.Imports) > 0 {
		b.WriteString("imports:\n")
		for _, imp := range f.Imports {
			path := imp.Path.Value
			if imp.Name != nil {
				b.WriteString(fmt.Sprintf("  %s %s\n", imp.Name.Name, path))
			} else {
				b.WriteString(fmt.Sprintf("  %s\n", path))
			}
		}
		b.WriteString("\n")
	}

	var types []string
	var funcs []string
	var methods []string
	var consts []string
	var vars []string

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					doc := extractDoc(d.Doc)
					sig := typeSignature(sp)
					entry := sig
					if doc != "" {
						entry = fmt.Sprintf("%s  // %s", sig, truncateStr(doc, 80))
					}
					types = append(types, entry)
				case *ast.ValueSpec:
					for _, name := range sp.Names {
						if !name.IsExported() {
							continue
						}
						kind := "var"
						if d.Tok == token.CONST {
							kind = "const"
						}
						entry := fmt.Sprintf("%s %s", kind, name.Name)
						if sp.Type != nil {
							entry += " " + formatFieldType(sp.Type)
						}
						if kind == "const" {
							consts = append(consts, entry)
						} else {
							vars = append(vars, entry)
						}
					}
				}
			}
		case *ast.FuncDecl:
			sig := funcSignature(d)
			doc := extractDoc(d.Doc)
			bodyLines := countBodyLines(fset, d)
			entry := fmt.Sprintf("%s  [%d lines]", sig, bodyLines)
			if doc != "" {
				entry += fmt.Sprintf("  // %s", truncateStr(doc, 80))
			}
			if d.Recv != nil {
				methods = append(methods, entry)
			} else {
				funcs = append(funcs, entry)
			}
		}
	}

	writeSection(&b, "types", types)
	writeSection(&b, "constants", consts)
	writeSection(&b, "variables", vars)
	writeSection(&b, "functions", funcs)
	writeSection(&b, "methods", methods)

	return b.String(), nil
}

// profileGenericFile extracts structure from non-Go code files using line analysis.
func (s *StudyService) profileGenericFile(fullPath, relPath, lang string) (string, error) {
	file, err := os.Open(fullPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var b strings.Builder
	b.WriteString(fmt.Sprintf("CODE FILE: %s [%s]\n\n", relPath, lang))

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	var imports []string
	var signatures []string
	var totalLines int
	var inImportBlock bool

	importRe := regexp.MustCompile(`^(?:import|from|require|use|using|#include|#import)\s+`)
	sigPatterns := []*regexp.Regexp{
		regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+\w+`),
		regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?class\s+\w+`),
		regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+\w+\s*=\s*(?:\(|async\s*\()`),
		regexp.MustCompile(`^(?:export\s+)?interface\s+\w+`),
		regexp.MustCompile(`^(?:export\s+)?type\s+\w+`),
		regexp.MustCompile(`^(?:export\s+)?enum\s+\w+`),
		regexp.MustCompile(`^def\s+\w+`),
		regexp.MustCompile(`^class\s+\w+`),
		regexp.MustCompile(`^(?:pub\s+)?(?:async\s+)?fn\s+\w+`),
		regexp.MustCompile(`^(?:pub\s+)?struct\s+\w+`),
		regexp.MustCompile(`^(?:pub\s+)?enum\s+\w+`),
		regexp.MustCompile(`^(?:pub\s+)?trait\s+\w+`),
		regexp.MustCompile(`^(?:pub\s+)?impl\s+`),
		regexp.MustCompile(`^(?:public|private|protected|internal)?\s*(?:static\s+)?(?:async\s+)?\w+\s+\w+\s*\(`),
		regexp.MustCompile(`^(?:@\w+\s+)*(?:public|private|protected)?\s*(?:static\s+)?(?:class|interface)\s+\w+`),
		regexp.MustCompile(`^message\s+\w+`),
		regexp.MustCompile(`^service\s+\w+`),
		regexp.MustCompile(`^type\s+\w+`),
		regexp.MustCompile(`^(?:CREATE|ALTER)\s+(?:TABLE|INDEX|FUNCTION|VIEW)\s+`),
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		totalLines++

		if totalLines > 2000 {
			break
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") && lang != "python" && lang != "shell" {
			continue
		}

		if importRe.MatchString(trimmed) || inImportBlock {
			imports = append(imports, trimmed)
			if strings.Contains(trimmed, "{") && !strings.Contains(trimmed, "}") {
				inImportBlock = true
			}
			if strings.Contains(trimmed, "}") {
				inImportBlock = false
			}
			continue
		}

		for _, re := range sigPatterns {
			if re.MatchString(trimmed) {
				sig := trimmed
				if len(sig) > 120 {
					sig = sig[:120] + "..."
				}
				signatures = append(signatures, sig)
				break
			}
		}
	}

	b.WriteString(fmt.Sprintf("total lines: %d\n\n", totalLines))

	if len(imports) > 0 {
		b.WriteString("imports:\n")
		for _, imp := range imports {
			if len(imp) > 100 {
				imp = imp[:100] + "..."
			}
			b.WriteString(fmt.Sprintf("  %s\n", imp))
		}
		b.WriteString("\n")
	}

	if len(signatures) > 0 {
		b.WriteString("exports/definitions:\n")
		for _, sig := range signatures {
			b.WriteString(fmt.Sprintf("  %s\n", sig))
		}
	}

	if len(signatures) == 0 && len(imports) == 0 {
		file2, err := os.Open(fullPath)
		if err == nil {
			defer file2.Close()
			scanner2 := bufio.NewScanner(file2)
			scanner2.Buffer(make([]byte, 64*1024), 64*1024)
			lineCount := 0
			b.WriteString("content (first 40 lines):\n")
			for scanner2.Scan() && lineCount < 40 {
				b.WriteString(scanner2.Text() + "\n")
				lineCount++
			}
		}
	}

	return b.String(), nil
}

// studyDocFile stores documentation/config files.
func (s *StudyService) studyDocFile(ctx context.Context, fullPath, relPath, lang string, projectID *uuid.UUID, sessionID uuid.UUID, result *StudyResult) {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("cannot read %s: %v", relPath, err))
		return
	}

	content := string(data)
	if len(content) < 10 {
		result.Skipped++
		return
	}

	if containsSecrets(content) {
		content = redactSecrets(content)
		result.Warnings = append(result.Warnings, fmt.Sprintf("REDACTED secrets in %s", relPath))
	}

	if len(content) > 4000 {
		content = content[:4000] + "\n... (truncated)"
	}

	importance := docFileImportance(relPath)
	tags := docFileTags(relPath, lang)
	memContent := fmt.Sprintf("PROJECT DOC: %s [%s]\n%s", relPath, lang, content)

	_, err = s.encode.Encode(ctx, &EncodeRequest{
		Content:    memContent,
		ProjectID:  projectID,
		AgentID:    "hippocampus-study",
		SessionID:  sessionID,
		Importance: importance,
		Tags:       tags,
	})
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("encode %s: %v", relPath, err))
		return
	}

	result.FilesRead++
	result.DocFiles++
	result.MemoriesCreated++
	result.Files = append(result.Files, relPath)
	result.ByLanguage[lang]++
}

// --- Helpers ---

func writeSection(b *strings.Builder, name string, items []string) {
	if len(items) == 0 {
		return
	}
	b.WriteString(name + ":\n")
	for _, item := range items {
		b.WriteString(fmt.Sprintf("  %s\n", item))
	}
	b.WriteString("\n")
}

func extractDoc(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	text := cg.Text()
	text = strings.TrimSpace(text)
	if idx := strings.IndexByte(text, '\n'); idx > 0 {
		text = text[:idx]
	}
	return text
}

func typeSignature(ts *ast.TypeSpec) string {
	switch t := ts.Type.(type) {
	case *ast.StructType:
		fields := countFields(t.Fields)
		return fmt.Sprintf("type %s struct {%d fields}", ts.Name.Name, fields)
	case *ast.InterfaceType:
		methods := countFields(t.Methods)
		return fmt.Sprintf("type %s interface {%d methods}", ts.Name.Name, methods)
	default:
		return fmt.Sprintf("type %s %s", ts.Name.Name, formatFieldType(ts.Type))
	}
}

func funcSignature(fd *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		recv := fd.Recv.List[0]
		b.WriteString("(")
		b.WriteString(formatFieldType(recv.Type))
		b.WriteString(") ")
	}
	b.WriteString(fd.Name.Name)
	b.WriteString("(")
	if fd.Type.Params != nil {
		writeFieldList(&b, fd.Type.Params)
	}
	b.WriteString(")")
	if fd.Type.Results != nil && len(fd.Type.Results.List) > 0 {
		b.WriteString(" ")
		if len(fd.Type.Results.List) > 1 {
			b.WriteString("(")
			writeFieldList(&b, fd.Type.Results)
			b.WriteString(")")
		} else {
			writeFieldList(&b, fd.Type.Results)
		}
	}
	return b.String()
}

func writeFieldList(b *strings.Builder, fl *ast.FieldList) {
	for i, f := range fl.List {
		if i > 0 {
			b.WriteString(", ")
		}
		if len(f.Names) > 0 {
			for j, name := range f.Names {
				if j > 0 {
					b.WriteString(", ")
				}
				b.WriteString(name.Name)
			}
			b.WriteString(" ")
		}
		b.WriteString(formatFieldType(f.Type))
	}
}

func countFields(fl *ast.FieldList) int {
	if fl == nil {
		return 0
	}
	n := 0
	for _, f := range fl.List {
		if len(f.Names) == 0 {
			n++
		} else {
			n += len(f.Names)
		}
	}
	return n
}

func countBodyLines(fset *token.FileSet, fd *ast.FuncDecl) int {
	if fd.Body == nil {
		return 0
	}
	start := fset.Position(fd.Body.Lbrace)
	end := fset.Position(fd.Body.Rbrace)
	return end.Line - start.Line
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func isSpecialFile(base string) bool {
	specials := []string{
		"makefile", "dockerfile", "taskfile.yml", "justfile",
		"package.json", "tsconfig.json", "pyproject.toml",
		"cargo.toml", "build.gradle", "pom.xml",
		".goreleaser.yml", ".golangci.yml",
	}
	for _, sp := range specials {
		if base == sp {
			return true
		}
	}
	return false
}

func codeFileImportance(relPath string) float64 {
	lower := strings.ToLower(relPath)
	switch {
	case strings.Contains(lower, "_test.go") || strings.Contains(lower, "_test.") || strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec."):
		return 0.55
	case strings.Contains(lower, "main.go") || strings.Contains(lower, "main.") || strings.Contains(lower, "index."):
		return 0.9
	case strings.Contains(lower, "cmd/"):
		return 0.85
	case strings.Contains(lower, "domain/") || strings.Contains(lower, "model"):
		return 0.85
	case strings.Contains(lower, "service") || strings.Contains(lower, "handler") || strings.Contains(lower, "controller"):
		return 0.8
	case strings.Contains(lower, "adapter/") || strings.Contains(lower, "repo"):
		return 0.75
	case strings.Contains(lower, "util") || strings.Contains(lower, "helper"):
		return 0.65
	case strings.Contains(lower, "generated") || strings.Contains(lower, "mock"):
		return 0.4
	default:
		return 0.7
	}
}

func codeFileTags(relPath, lang string) []string {
	tags := []string{"code", lang}
	lower := strings.ToLower(relPath)

	if strings.Contains(lower, "_test.") || strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec.") {
		tags = append(tags, "test")
	}
	if strings.Contains(lower, "cmd/") || strings.Contains(lower, "main") {
		tags = append(tags, "entrypoint")
	}
	if strings.Contains(lower, "domain/") || strings.Contains(lower, "model") {
		tags = append(tags, "domain")
	}
	if strings.Contains(lower, "handler") || strings.Contains(lower, "controller") || strings.Contains(lower, "route") {
		tags = append(tags, "api")
	}
	if strings.Contains(lower, "repo") || strings.Contains(lower, "store") || strings.Contains(lower, "database") {
		tags = append(tags, "persistence")
	}
	if strings.Contains(lower, "service") {
		tags = append(tags, "business_logic")
	}
	if strings.Contains(lower, "adapter/") || strings.Contains(lower, "infra") {
		tags = append(tags, "infrastructure")
	}

	dir := filepath.Dir(relPath)
	if dir != "." {
		tags = append(tags, "dir:"+filepath.ToSlash(dir))
	}

	return tags
}

func docFileImportance(relPath string) float64 {
	switch {
	case strings.HasSuffix(relPath, ".mdc"):
		return 0.9
	case strings.Contains(relPath, "README"):
		return 0.85
	case strings.Contains(relPath, "CLAUDE"):
		return 0.85
	case strings.Contains(relPath, "docker-compose"):
		return 0.8
	case strings.Contains(relPath, "go.mod") || strings.Contains(relPath, "package.json") || strings.Contains(relPath, "Cargo.toml"):
		return 0.75
	case strings.Contains(relPath, "Makefile") || strings.Contains(relPath, "Dockerfile"):
		return 0.8
	case strings.Contains(relPath, ".env"):
		return 0.6
	default:
		return 0.7
	}
}

func docFileTags(relPath, lang string) []string {
	tags := []string{"project_knowledge", lang}
	switch {
	case strings.HasSuffix(relPath, ".mdc"):
		tags = append(tags, "cursor_rule")
	case strings.Contains(relPath, "README"):
		tags = append(tags, "documentation")
	case strings.Contains(relPath, "docker"):
		tags = append(tags, "infrastructure")
	case strings.Contains(relPath, "go.mod") || strings.Contains(relPath, "package.json"):
		tags = append(tags, "dependencies")
	case strings.Contains(relPath, "CLAUDE"):
		tags = append(tags, "claude_code")
	case strings.Contains(relPath, "docs/"):
		tags = append(tags, "documentation")
	}
	return tags
}

func containsSecrets(content string) bool {
	lower := strings.ToLower(content)
	for _, pattern := range secretPatterns {
		idx := strings.Index(lower, pattern)
		if idx < 0 {
			continue
		}
		after := lower[idx+len(pattern):]
		if len(after) > 0 && (after[0] == '=' || after[0] == ':' || after[0] == '"') {
			return true
		}
	}
	return false
}

func redactSecrets(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		for _, pattern := range secretPatterns {
			if strings.Contains(lower, pattern) {
				if idx := strings.IndexAny(line, "=:"); idx >= 0 {
					lines[i] = line[:idx+1] + " ***REDACTED***"
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}
