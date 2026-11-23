package report

import "time"

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func rng(from, to string) Range { return Range{From: day(from), To: day(to)} }
