package app

import (
	"os"
	"strings"
	"time"
)

// localtime is the system zone link consulted when TZ is unset.
const localtime = "/etc/localtime"

// LocalLocation returns the server time zone under its IANA name (spec 2: the time zone
// comes only from TZ). time.Local itself is named "Local", which the UI cannot use.
func LocalLocation() *time.Location {
	return namedLocation(os.Getenv("TZ"), os.Readlink)
}

func namedLocation(tz string, readlink func(string) (string, error)) *time.Location {
	name := strings.TrimPrefix(tz, ":")
	if name == "" {
		if target, err := readlink(localtime); err == nil {
			_, name, _ = strings.Cut(target, "zoneinfo/")
		}
	}
	if name == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local
	}
	return loc
}
