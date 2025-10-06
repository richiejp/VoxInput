package conf

import "os"


func GetPrefixedEnv(prefixes []string, name string, fallback string) (val string) {
	for _, p := range prefixes {
		var n string
		if p == "" {
			n = name
		} else {
			n = p + "_" + name
		}
		if val = os.Getenv(n); val != "" {
			return val
		}
	}

	return fallback
}

func GetOpenaiEnv(name string, fallback string) string {
	return GetPrefixedEnv([]string{"VOXINPUT", "OPENAI"}, name, fallback)
}

