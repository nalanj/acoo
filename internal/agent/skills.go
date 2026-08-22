package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nalanj/acoo/internal/log"
)

// Skill represents a loaded skill
type Skill struct {
	Name        string
	Description string
	Location    string // Absolute path to the SKILL.md file
	Content     string // Full skill content (markdown)
}

// Skills returns all available skills from the skills directory
// It scans for subdirectories containing SKILL.md files
func Skills(skillsDir string) []Skill {
	var skills []Skill

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.System().Warn("read_skills_dir_failed", log.F("dir", skillsDir), log.F("error", err))
		}
		return skills
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillPath := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillPath); os.IsNotExist(err) {
			continue
		}

		skill, err := loadSkill(skillPath)
		if err != nil {
			log.System().Warn("load_skill_failed", log.F("path", skillPath), log.F("error", err))
			continue
		}

		skills = append(skills, skill)
	}

	return skills
}

// loadSkill loads a single skill from a file
func loadSkill(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}

	content := string(data)

	// Parse frontmatter
	name := extractFrontmatterField(content, "name")
	description := extractFrontmatterField(content, "description")
	body := extractBody(content)

	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	if description == "" {
		description = "No description"
	}

	return Skill{
		Name:        name,
		Description: description,
		Location:    path,
		Content:     body,
	}, nil
}

// extractFrontmatterField extracts a field value from YAML frontmatter
func extractFrontmatterField(content, field string) string {
	lines := strings.Split(content, "\n")
	inFrontmatter := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			break // End of frontmatter
		}

		if inFrontmatter {
			prefix := field + ":"
			if strings.HasPrefix(trimmed, prefix) {
				value := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
				// Remove quotes if present
				value = strings.Trim(value, "\"")
				return value
			}
		}
	}

	return ""
}

// extractBody extracts the markdown body after frontmatter
func extractBody(content string) string {
	lines := strings.Split(content, "\n")
	inFrontmatter := false
	bodyLines := []string{}
	afterFrontmatter := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			afterFrontmatter = true
			continue
		}

		if afterFrontmatter || !inFrontmatter {
			bodyLines = append(bodyLines, line)
		}
	}

	return strings.TrimSpace(strings.Join(bodyLines, "\n"))
}

// BuildSkillsSection creates the skills section for the system prompt
func BuildSkillsSection(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "", "Skills you have access to:")
	lines = append(lines, "When a task matches a skill's description, use your file-read tool to load the SKILL.md at the listed location.")
	lines = append(lines, "")

	for _, skill := range skills {
		lines = append(lines, fmt.Sprintf("- %s - %s (%s)", skill.Name, skill.Description, skill.Location))
	}

	return strings.Join(lines, "\n")
}
