package config

// Job represents a job definition
type Job struct {
	Name      string `yaml:"name"`
	SourceFile string `yaml:"-"`
	Body      string `yaml:"-"` // The task prompt
}
