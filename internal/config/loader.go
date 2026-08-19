package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FrontMatterRegex matches YAML front matter
var FrontMatterRegex = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n(.*)`)

// LoadAll loads agents and jobs from directories
func LoadAll(agentsDir, jobsDir string) ([]*Agent, map[string]*Job, error) {
	agents, err := LoadAgents(agentsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("loading agents: %w", err)
	}

	jobs, err := LoadJobs(jobsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("loading jobs: %w", err)
	}

	// Link jobs to agents
	for _, agent := range agents {
		agent.JobsMap = make(map[string]*Job)
		for _, jobName := range agent.Jobs {
			if job, ok := jobs[jobName]; ok {
				agent.JobsMap[jobName] = job
			}
		}
	}

	return agents, jobs, nil
}

// LoadAgents loads all agent configs from a directory
func LoadAgents(dir string) ([]*Agent, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading agents dir: %w", err)
	}

	var agents []*Agent
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		agent, err := LoadAgentFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s: %v\n", path, err)
			continue
		}

		if agent != nil {
			agents = append(agents, agent)
		}
	}

	sort.Slice(agents, func(i, j int) bool {
		return agents[i].Name < agents[j].Name
	})

	return agents, nil
}

// LoadAgentFile loads a single agent config file
func LoadAgentFile(path string) (*Agent, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	return ParseAgentContent(string(content), path)
}

// ParseAgentContent parses markdown content with optional front matter
func ParseAgentContent(content string, sourceFile string) (*Agent, error) {
	matches := FrontMatterRegex.FindStringSubmatch(content)
	if matches == nil {
		return nil, fmt.Errorf("missing front matter in %s", sourceFile)
	}

	frontMatter := matches[1]
	body := matches[2]

	var agent Agent
	if err := yaml.Unmarshal([]byte(frontMatter), &agent); err != nil {
		return nil, fmt.Errorf("parsing front matter: %w", err)
	}

	if agent.Name == "" {
		agent.Name = strings.TrimSuffix(filepath.Base(sourceFile), ".md")
	}

	agent.SourceFile = sourceFile
	agent.Body = strings.TrimSpace(body)

	return &agent, nil
}

// LoadJobs loads all job configs from a directory
func LoadJobs(dir string) (map[string]*Job, error) {
	jobs := make(map[string]*Job)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return jobs, nil
		}
		return nil, fmt.Errorf("reading jobs dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		job, err := LoadJobFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s: %v\n", path, err)
			continue
		}

		if job != nil {
			jobs[job.Name] = job
		}
	}

	return jobs, nil
}

// LoadJobFile loads a single job config file
func LoadJobFile(path string) (*Job, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	return ParseJobContent(string(content), path)
}

// ParseJobContent parses markdown content with optional front matter
func ParseJobContent(content string, sourceFile string) (*Job, error) {
	matches := FrontMatterRegex.FindStringSubmatch(content)
	if matches == nil {
		// No front matter - use filename as name
		name := strings.TrimSuffix(filepath.Base(sourceFile), ".md")
		return &Job{
			Name:      name,
			SourceFile: sourceFile,
			Body:      strings.TrimSpace(content),
		}, nil
	}

	frontMatter := matches[1]
	body := matches[2]

	var job Job
	if err := yaml.Unmarshal([]byte(frontMatter), &job); err != nil {
		return nil, fmt.Errorf("parsing front matter: %w", err)
	}

	if job.Name == "" {
		job.Name = strings.TrimSuffix(filepath.Base(sourceFile), ".md")
	}

	job.SourceFile = sourceFile
	job.Body = strings.TrimSpace(body)

	return &job, nil
}

// ListAgents returns agent info for listing
func ListAgents(agentsDir, jobsDir string) ([]AgentInfo, error) {
	agents, _, err := LoadAll(agentsDir, jobsDir)
	if err != nil {
		return nil, err
	}

	var infos []AgentInfo
	for _, a := range agents {
		var jobs []string
		for _, jobName := range a.Jobs {
			if job, ok := a.JobsMap[jobName]; ok {
				model := job.Model
				if model != "" {
					jobs = append(jobs, fmt.Sprintf("%s: %s (model: %s, provider: %s)", jobName, job.Schedule, model, job.Provider))
				} else {
					jobs = append(jobs, fmt.Sprintf("%s: %s (provider: %s)", jobName, job.Schedule, job.Provider))
				}
			} else {
				jobs = append(jobs, fmt.Sprintf("%s: <not found>", jobName))
			}
		}
		infos = append(infos, AgentInfo{
			Name:   a.Name,
			Jobs:   jobs,
			Source: a.SourceFile,
		})
	}

	return infos, nil
}

// AgentInfo represents basic agent info for listing
type AgentInfo struct {
	Name   string
	Jobs   []string
	Source string
}
