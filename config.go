package main

import "time"

type config struct {
	pkg      string
	funcName string
	prune    bool
	verbose  bool
	timeout  time.Duration
}
