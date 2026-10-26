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

// Get после Put должен возвращать тот же объект (без GC-давления).
func TestGetReturnsPutObject(t *testing.T) {
	p := New(func() *Buffer { return new(Buffer) })
	b := p.Get()
	*b = append(*b, 1, 2, 3)
	p.Put(b)
	b2 := p.Get()
	if b2 != b {
		t.Fatal("expected Get to return the same object that was Put")
	}
	if len(*b2) != 0 {
		t.Fatalf("expected Reset state, got %d bytes", len(*b2))
	}
}

// nil-фабрика на пустом пуле: Get возвращает var zero T, не паникует.
func TestNilFactoryReturnsZero(t *testing.T) {
	p := New[*Buffer](nil)
	if got := p.Get(); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

// nil-фабрика + Put: Reset вызывается, Get возвращает объект обратно.
func TestNilFactoryAfterPut(t *testing.T) {
	p := New[*Buffer](nil)
	b := new(Buffer)
	*b = append(*b, 1, 2, 3)
	p.Put(b)
	if len(*b) != 0 {
		t.Fatal("expected Reset to be called on Put")
	}
	if got := p.Get(); got != b {
		t.Fatal("expected Get to return the object that was Put")
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
