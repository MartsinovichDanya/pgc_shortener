package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/MartsinovichDanya/pgc_shortener/internal/utils"
)

func BenchmarkFileStoreSave(b *testing.B) {
	s, _ := NewFileStore("") // без файла
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		if err := s.Save(ctx, utils.NewUUID(), fmt.Sprintf("https://bench.example.com/%d", i), "u1"); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
}

func BenchmarkFileStoreGet(b *testing.B) {
	s, _ := NewFileStore("")
	ctx := context.Background()
	const n = 10000
	ids := make([]string, n)
	for i := range ids {
		ids[i] = utils.NewUUID()
		_ = s.Save(ctx, ids[i], fmt.Sprintf("https://bench.example.com/%d", i), "u1")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Get(ctx, ids[i%n]); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
}

func BenchmarkFileStoreSaveWithFile(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "bench-db.json")
	_ = os.Remove(path)
	s, _ := NewFileStore(path)
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		if err := s.Save(ctx, utils.NewUUID(), fmt.Sprintf("https://bench-file.example.com/%d", i), "u1"); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
}
