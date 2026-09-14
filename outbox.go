package main

import (
	"context"
	"log"
	"math"
	"time"
)

func startOutboxDispatcher(ctx context.Context, repository JobRepository, queue Queue) {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			if err := dispatchOutboxBatch(repository, queue); err != nil {
				log.Printf("outbox dispatcher: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func dispatchOutboxBatch(repository JobRepository, queue Queue) error {
	entries, err := repository.ListPendingOutbox(50)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := queue.Publish(entry.Job); err != nil {
			attempts := entry.Attempts + 1
			backoff := time.Duration(math.Min(float64(attempts), 6)) * time.Second
			if updateErr := repository.MarkOutboxFailed(entry.ID, attempts, time.Now().Add(backoff), err.Error()); updateErr != nil {
				return updateErr
			}
			continue
		}
		if err := repository.MarkOutboxSent(entry.ID); err != nil {
			return err
		}
	}
	return nil
}