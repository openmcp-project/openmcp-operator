package config

var (
	environment string
)

func SetEnvironment(env string) {
	if environment != "" {
		panic("environment already set")
	}
	environment = env
}

func Environment() string {
	if environment == "" {
		panic("environment not set")
	}
	return environment
}
