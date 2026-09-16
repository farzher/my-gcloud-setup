package main

import (
	"context"
	"fmt"
	"sort"
	"time"
)

func detectSession() (cloudState, error) {
	var s cloudState
	if !hasExecutable("gcloud") {
		return s, nil
	}
	s.Gcloud = true

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	type result struct {
		output commandResult
		err    error
	}
	allCh := make(chan result, 1)
	activeCh := make(chan result, 1)
	go func() {
		r, err := run(ctx, "gcloud", "auth", "list", "--format=value(account)")
		allCh <- result{r, err}
	}()
	go func() {
		r, err := run(ctx, "gcloud", "auth", "list", "--filter=status:ACTIVE", "--format=value(account)")
		activeCh <- result{r, err}
	}()

	all := <-allCh
	active := <-activeCh
	if active.err != nil {
		return s, fmt.Errorf("gcloud auth: %w\n%s", active.err, usefulOutput(active.output))
	}
	if all.err == nil {
		s.Accounts = uniqueLines(all.output.Stdout)
		sort.Strings(s.Accounts)
	}
	s.Account = firstLine(active.output.Stdout)
	return s, nil
}
