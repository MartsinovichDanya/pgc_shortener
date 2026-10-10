package utils

import "testing"

func BenchmarkGenerateID(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := GenerateID(8); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNewUUID(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NewUUID()
	}
}
