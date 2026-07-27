package invalid

import "os"

func readGetenv() string {
	return os.Getenv("DATABASE_URL") // want `do not call os.Getenv directly; read configuration through the config package`
}

func readLookupEnv() (string, bool) {
	return os.LookupEnv("DATABASE_URL") // want `do not call os.LookupEnv directly; read configuration through the config package`
}
