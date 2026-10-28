package main

import "testing"

// Put должен вызывать Reset на объекте.
func TestPutResetsState(t *testing.T) {
	p := New(func() *Buffer { return new(Buffer) })
	b := p.Get()
	*b = append(*b, "hello"...)
	if len(*b) != 5 {
		t.Fatalf("expected 5 bytes, got %d", len(*b))
	}
	p.Put(b)
	if len(*b) != 0 {
		t.Fatalf("expected Reset to clear state, got %d bytes", len(*b))
	}
}

// После Put объект чистый. То, что Get может вернуть другой объект
// (GC вправе очистить пул между вызовами), на корректность не влияет:
// проверяем именно гарантируемый инвариант -- любой объект из Get чист.
func TestGetReturnsCleanObject(t *testing.T) {
	p := New(func() *Buffer { return new(Buffer) })
	b := p.Get()
	*b = append(*b, 1, 2, 3)
	p.Put(b)
	// Reset точно был вызван на b -- это гарантия Put.
	if len(*b) != 0 {
		t.Fatalf("expected Reset to clear state, got %d bytes", len(*b))
	}
	b2 := p.Get()
	// b2 может быть как b, так и свежим объектом из фабрики --
	// в обоих случаях он обязан быть чистым.
	if b2 == nil {
		t.Fatal("expected non-nil object")
	}
	if len(*b2) != 0 {
		t.Fatalf("expected clean object from Get, got %d bytes", len(*b2))
	}
}

// nil-фабрика на пустом пуле: Get возвращает var zero T, не паникует.
func TestNilFactoryReturnsZero(t *testing.T) {
	p := New[*Buffer](nil)
	if got := p.Get(); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

// nil-фабрика + Put: Reset вызывается при Put. Get может вернуть
// тот же объект, nil (если GC очистил пул) или... в любом случае,
// что бы ни вернулось -- оно должно быть чистым.
func TestNilFactoryAfterPut(t *testing.T) {
	p := New[*Buffer](nil)
	b := new(Buffer)
	*b = append(*b, 1, 2, 3)
	p.Put(b)
	if len(*b) != 0 {
		t.Fatal("expected Reset to be called on Put")
	}
	// Указательное равенство здесь не проверяем: sync.Pool не гарантирует,
	// что Get вернёт именно b. Гарантия -- чистота объекта (или nil).
	if got := p.Get(); got != nil && len(*got) != 0 {
		t.Fatalf("expected clean object or nil, got %d bytes", len(*got))
	}
}

// Множество циклов Get/Put не должно ломать инвариант "объект чистый".
func TestManyObjects(t *testing.T) {
	p := New(func() *Buffer { return new(Buffer) })
	const n = 1000
	for i := 0; i < n; i++ {
		b := p.Get()
		if b == nil {
			t.Fatal("pool returned nil")
		}
		if len(*b) != 0 {
			t.Fatalf("expected clean buffer, got %d bytes", len(*b))
		}
		*b = append(*b, byte(i))
		p.Put(b)
	}
}
