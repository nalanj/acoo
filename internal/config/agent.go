package config

// Agent represents an agent configuration
type Agent struct {
	Env      map[string]string `yaml:"env"`
	Jobs     map[string]string `yaml:"jobs"` // job name -> schedule

	SourceFile string `yaml:"-"`
	Name      string `yaml:"-"` // Derived from filename
	Body      string `yaml:"-"` // The system prompt

	JobsMap map[string]*Job `yaml:"-"` // Resolved job objects
}

// GetEnv returns the environment variables for this agent
func (a *Agent) GetEnv() map[string]string {
	return a.Env
}
