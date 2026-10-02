package main

import "time"

func timeSleep(ms int) <-chan time.Time {
	return time.After(time.Duration(ms) * time.Millisecond)
}
