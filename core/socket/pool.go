package socket

import (
	"context"
	"sync"
)

func Pool[Message any](ctx context.Context, in chan Message, count int, fn func(Message) error) {

	repeaters := make([]chan Message, count)
	for i := range repeaters {
		repeaters[i] = make(chan Message, 1000)

		go func(i int) {
			for {
				select {
				case <-ctx.Done():
					close(repeaters[i])
					return
				case m, ok := <-repeaters[i]:
					if !ok {
						close(repeaters[i])
						return
					}
					fn(m)
				}
			}
		}(i)
	}

	go func() {
		next := 0
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-in:
				repeaters[next] <- m
				if next > count-1 {
					next = 0
				}
			}
		}
	}()

}
func PoolSync[Message any](ctx context.Context, wg *sync.WaitGroup, in chan Message, count int, fn func(Message) error) {

	repeaters := make([]chan Message, count)
	for i := range repeaters {
		repeaters[i] = make(chan Message, 1000)

		go func(i int) {
			for {
				select {
				case <-ctx.Done():
					close(repeaters[i])
					return
				case m, ok := <-repeaters[i]:
					if !ok {
						close(repeaters[i])
						return
					}
					fn(m)
					wg.Done()
				}
			}
		}(i)
	}

	go func() {
		next := 0
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-in:
				repeaters[next] <- m
				if next > count-2 {
					next = 0
				} else {
					next++
				}
			}
		}
	}()

	wg.Wait()

}
