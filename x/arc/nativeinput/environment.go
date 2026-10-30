package nativeinput

import "os"

// ChildEnvironment never returns nil: nil would inherit the complete App env.
func ChildEnvironment() []string {
	env := make([]string, 0, 3)
	for _, key := range []string{"LANG", "LC_ALL", "TZ"} {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return env
}
