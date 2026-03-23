package app

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ProjectIdentity holds detected project metadata.
type ProjectIdentity struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Language    string `json:"language"`
	Description string `json:"description"`
	RootPath    string `json:"root_path"`
}

// DetectProject auto-detects project metadata from common project files.
// Examines go.mod, package.json, Cargo.toml, .git/config, pyproject.toml, etc.
func DetectProject(rootPath string) *ProjectIdentity {
	if rootPath == "" {
		return nil
	}

	id := &ProjectIdentity{RootPath: rootPath}

	id.Name = filepath.Base(rootPath)
	id.Slug = slugify(id.Name)

	if goMod := tryReadFile(filepath.Join(rootPath, "go.mod")); goMod != "" {
		id.Language = "go"
		if modName := extractGoModule(goMod); modName != "" {
			parts := strings.Split(modName, "/")
			id.Name = parts[len(parts)-1]
			id.Slug = slugify(id.Name)
		}
	} else if pkgJSON := tryReadFile(filepath.Join(rootPath, "package.json")); pkgJSON != "" {
		id.Language = "typescript"
		var pkg struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if json.Unmarshal([]byte(pkgJSON), &pkg) == nil {
			if pkg.Name != "" {
				id.Name = pkg.Name
				id.Slug = slugify(pkg.Name)
			}
			id.Description = pkg.Description
		}
	} else if cargoToml := tryReadFile(filepath.Join(rootPath, "Cargo.toml")); cargoToml != "" {
		id.Language = "rust"
		id.Name = extractTomlValue(cargoToml, "name")
		if id.Name != "" {
			id.Slug = slugify(id.Name)
		}
	} else if fileExists(filepath.Join(rootPath, "pyproject.toml")) || fileExists(filepath.Join(rootPath, "setup.py")) || fileExists(filepath.Join(rootPath, "requirements.txt")) {
		id.Language = "python"
	} else if fileExists(filepath.Join(rootPath, "pom.xml")) || fileExists(filepath.Join(rootPath, "build.gradle")) {
		id.Language = "java"
	}

	if desc := tryReadFile(filepath.Join(rootPath, "README.md")); desc != "" && id.Description == "" {
		lines := strings.SplitN(desc, "\n", 5)
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") && len(line) > 10 {
				if len(line) > 200 {
					line = line[:200]
				}
				id.Description = line
				break
			}
		}
	}

	return id
}

func extractGoModule(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module"))
		}
	}
	return ""
}

func extractTomlValue(content, key string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, key+" =") || strings.HasPrefix(line, key+"=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				val := strings.TrimSpace(parts[1])
				return strings.Trim(val, "\"'")
			}
		}
	}
	return ""
}

func slugify(name string) string {
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r == '_' || r == ' ' || r == '.' || r == '/' || r == '@' {
			return '-'
		}
		return -1
	}, name)
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	name = strings.Trim(name, "-")
	if name == "" {
		name = "unnamed"
	}
	return name
}

func tryReadFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) > 4096 {
		data = data[:4096]
	}
	return string(data)
}
