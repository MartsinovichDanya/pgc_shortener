package main

import (
	"fmt"
	"sync"
)

type Resettable interface {
	Reset()
}

// Pool — типобезопасная обёртка над sync.Pool.
type Pool[T Resettable] struct {
	pool    sync.Pool
	newFunc func() T
}

func New[T Resettable](f func() T) *Pool[T] {
	p := &Pool[T]{newFunc: f}
	// sync.Pool допускает New == nil: в этом случае Get вернёт nil,
	// а мы превратим его в var zero T в Get.
	if f != nil {
		p.pool.New = func() any { return p.newFunc() }
	}
	return p
}

func (p *Pool[T]) Get() T {
	v := p.pool.Get()
	if v == nil {
		var zero T
		return zero
	}
	return v.(T)
}

func (p *Pool[T]) Put(v T) {
	v.Reset()
	p.pool.Put(v)
}

// Buffer — пример типа с методом Reset().
type Buffer []byte

func (b *Buffer) Reset() { *b = (*b)[:0] }

func main() {
	pool := New(func() *Buffer { return new(Buffer) })

	buf := pool.Get()
	*buf = append(*buf, "hello"...)
	fmt.Println(len(*buf)) // 5

	pool.Put(buf)

	buf2 := pool.Get()
	fmt.Println(len(*buf2)) // 0 — Reset очистил состояние
}
