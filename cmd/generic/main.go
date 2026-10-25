package main

import "fmt"

type Resettable interface {
	Reset()
}

type Pool[T Resettable] struct {
	newFunc func() T // фабрика новых объектов, когда пул пуст
	ch      chan T   // хранилище освобождённых объектов
}

func New[T Resettable](f func() T) *Pool[T] {
	return &Pool[T]{
		newFunc: f,
		ch:      make(chan T, 1024), // размер буфера — на ваше усмотрение
	}
}

func (p *Pool[T]) Get() T {
	select {
	case v := <-p.ch: // в пуле что-то есть — берём готовый объект
		return v
	default: // пул пуст — создаём новый через фабрику
		return p.newFunc()
	}
}

func (p *Pool[T]) Put(v T) {
	v.Reset() // очищаем состояние ОБЪЕКТА — это смысл всего задания
	select {
	case p.ch <- v: // пул не переполнен — кладём обратно
	default: // пул полон — просто отпускаем объект (заберёт GC)
	}
}

// Buffer — пример типа с методом Reset().
type Buffer []byte

func (b *Buffer) Reset() { *b = (*b)[:0] }

func main() {
	pool := New(func() *Buffer { return new(Buffer) })

	buf := pool.Get() // новый объект (пул пуст)
	*buf = append(*buf, "hello"...)
	fmt.Println(len(*buf)) // 5

	pool.Put(buf) // сброс + возврат в пул

	buf2 := pool.Get()      // тот же самый объект из пула
	fmt.Println(len(*buf2)) // 0 — состояние сброшено через Reset()
}
